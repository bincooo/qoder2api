package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"qoder2api/internal/cosy"
)

const chatURL = "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"

type Bridge struct {
	sess     *cosy.SessionContext
	template []byte
}

func NewBridge(sess *cosy.SessionContext, template []byte) *Bridge {
	return &Bridge{sess: sess, template: template}
}

// Handler serves the /v1/chat/completions endpoint, mirroring Java handleChat.
func (b *Bridge) Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Stream   bool   `json:"stream"`
		Model    string `json:"model"`
		Messages []any  `json:"messages"`
		Tools    []any  `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	// model is the display name echoed back to the OpenAI client; the upstream
	// key is resolved separately and only fed to the Qoder model_config.
	model := req.Model
	if model == "" {
		model = "lite"
	}
	upstreamKey := resolveModelKey(model)

	uuids := []string{cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID()}
	bodyBytes, err := FillTemplate(b.template, uuids, time.Now().UnixMilli())
	if err != nil {
		writeErr(w, err)
		return
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		writeErr(w, err)
		return
	}
	nid := cosy.NewUUID()
	body["request_id"] = nid
	body["chat_record_id"] = nid
	body["request_set_id"] = cosy.NewUUID()
	body["session_id"] = cosy.NewUUID()
	body["stream"] = true
	body["aliyun_user_type"] = b.sess.Identity.UserType
	mc := body["model_config"].(map[string]any)
	mc["key"] = upstreamKey
	biz := body["business"].(map[string]any)
	biz["id"] = cosy.NewUUID()
	biz["begin_at"] = time.Now().UnixMilli()

	prompt := ExtractLatestUserPrompt(req.Messages)
	chatCtx := body["chat_context"].(map[string]any)
	chatCtx["text"].(map[string]any)["text"] = prompt
	chatCtx["extra"].(map[string]any)["originalContent"].(map[string]any)["text"] = prompt
	name := prompt
	if len(name) > 30 {
		name = name[:30]
	}
	biz["name"] = name
	toolsEnabled := len(req.Tools) > 0
	tmpl := body["messages"].([]any)
	body["messages"] = BuildQoderMessages(tmpl, req.Messages, prompt, toolsEnabled)
	if toolsEnabled {
		body["tools"] = req.Tools
	}

	bodyEncoded := []byte(cosy.Encode([]byte(mustJSON(body))))
	extra := map[string]string{
		"x-model-key":    upstreamKey,
		"x-model-source": mc["source"].(string),
	}

	reqID := "chatcmpl-" + strings.ReplaceAll(cosy.NewUUID(), "-", "")[:24]
	created := time.Now().Unix()

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		acc := newStreamAccumulator(toolsEnabled, func(role, content string, tc []any) {
			writeSseChunk(w, reqID, created, model, role, content, tc)
			if fl != nil {
				fl.Flush()
			}
		})
		err := b.sess.SignedPostStream(context.Background(), chatURL, bodyEncoded, extra, func(line string) {
			if !strings.HasPrefix(line, "data:") {
				return
			}
			delta := extractDelta(strings.TrimSpace(line[5:]))
			if delta.isEmpty() {
				return
			}
			acc.Accept(*delta)
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		acc.Flush()
		// terminal chunk mirrors Java: empty delta, finish_reason from accumulator.
		chunk := makeChunk(reqID, created, model)
		choice := chunk["choices"].([]any)[0].(map[string]any)
		choice["finish_reason"] = acc.FinishReason()
		choice["delta"] = map[string]any{}
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	// non-streaming, mirroring Java lines 132-183.
	var full strings.Builder
	acc := &ToolCallAccumulator{}
	if err := b.sess.SignedPostStream(context.Background(), chatURL, bodyEncoded, extra, func(line string) {
		if !strings.HasPrefix(line, "data:") {
			return
		}
		delta := extractDelta(strings.TrimSpace(line[5:]))
		if delta == nil {
			return
		}
		if delta.Content != "" {
			full.WriteString(delta.Content)
		}
		if len(delta.ToolCalls) > 0 {
			acc.Append(delta.ToolCalls)
		}
	}); err != nil {
		writeErr(w, err)
		return
	}

	var fallbackToolCalls []any
	if acc.IsEmpty() && toolsEnabled {
		fallbackToolCalls = ParseToolCallsText(full.String())
	}

	msg := map[string]any{"role": "assistant"}
	switch {
	case fallbackToolCalls != nil:
		msg["content"] = nil
		msg["tool_calls"] = fallbackToolCalls
	case full.Len() == 0 && !acc.IsEmpty():
		msg["content"] = nil
	default:
		msg["content"] = full.String()
	}
	if !acc.IsEmpty() {
		msg["tool_calls"] = acc.Snapshot()
	}

	finishReason := "stop"
	if !acc.IsEmpty() || fallbackToolCalls != nil {
		finishReason = "tool_calls"
	}

	resp := map[string]any{
		"id":      reqID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finishReason}},
		"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}
	writeJSON(w, resp)
}

// extractDelta parses the {body: <json-string>} SSE wrapper then the inner
// {choices:[{delta:{role,content,tool_calls}}]} shape, mirroring Java.
func extractDelta(dataLine string) *Delta {
	var wrapper struct {
		Body string `json:"body"`
	}
	if json.Unmarshal([]byte(dataLine), &wrapper) != nil || wrapper.Body == "" {
		return nil
	}
	var inner struct {
		Choices []struct {
			Delta struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []any  `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(wrapper.Body), &inner) != nil {
		return nil
	}
	for _, ch := range inner.Choices {
		d := ch.Delta
		if d.Role != "" || d.Content != "" || len(d.ToolCalls) > 0 {
			return &Delta{Role: d.Role, Content: d.Content, ToolCalls: d.ToolCalls}
		}
	}
	return nil
}

// makeChunk mirrors Java makeChunk: a shell chunk with an empty delta and
// null finish_reason, to be mutated by callers.
func makeChunk(id string, created int64, model string) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": nil}},
	}
}

// writeSseChunk mirrors Java writeSseChunk: build the chunk and conditionally
// add role/content/tool_calls to its delta.
func writeSseChunk(w http.ResponseWriter, id string, created int64, model, role, content string, tc []any) {
	chunk := makeChunk(id, created, model)
	delta := chunk["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if role != "" {
		delta["role"] = role
	}
	if content != "" {
		delta["content"] = content
	}
	if len(tc) > 0 {
		delta["tool_calls"] = tc
	}
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

func writeErr(w http.ResponseWriter, err error) {
	msg := strings.ReplaceAll(err.Error(), `"`, `\"`)
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "qoder_error"}})
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(body)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
