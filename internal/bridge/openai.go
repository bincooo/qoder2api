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
	if err = json.Unmarshal(bodyBytes, &body); err != nil {
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
		var lastDelta *Delta
		acc := newStreamAccumulator(toolsEnabled, func(role, content string, tc []any) {
			writeSseChunk(w, reqID, created, model, role, content, tc)
			if fl != nil {
				fl.Flush()
			}
		})
		err = b.sess.SignedPostStream(context.Background(), chatURL, bodyEncoded, extra, func(line string) {
			if !strings.HasPrefix(line, "data:") {
				return
			}
			delta := extractDelta(strings.TrimSpace(line[5:]))
			if delta == nil {
				return
			}
			lastDelta = delta
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

		// Set usage from the last delta if available
		if lastDelta != nil && (lastDelta.PromptTokens > 0 || lastDelta.CompletionTokens > 0 || lastDelta.TotalTokens > 0) {
			choice["usage"] = map[string]any{
				"prompt_tokens":     lastDelta.PromptTokens,
				"completion_tokens": lastDelta.CompletionTokens,
				"total_tokens":      lastDelta.TotalTokens,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": lastDelta.CachedTokens,
				},
			}
		} else {
			choice["usage"] = map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
		}
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	// non-streaming, mirroring Java lines 132-183.
	var full strings.Builder
	acc := &ToolCallAccumulator{}
	var lastDelta *Delta
	if err := b.sess.SignedPostStream(context.Background(), chatURL, bodyEncoded, extra, func(line string) {
		if !strings.HasPrefix(line, "data:") {
			return
		}
		delta := extractDelta(strings.TrimSpace(line[5:]))
		if delta == nil {
			return
		}
		lastDelta = delta
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

	// Build usage from response_meta.usage
	usage := map[string]any{
		"prompt_tokens":     0,
		"completion_tokens": 0,
		"total_tokens":      0,
	}
	if lastDelta != nil && (lastDelta.PromptTokens > 0 || lastDelta.CompletionTokens > 0 || lastDelta.TotalTokens > 0) {
		usage = map[string]any{
			"prompt_tokens":     lastDelta.PromptTokens,
			"completion_tokens": lastDelta.CompletionTokens,
			"total_tokens":      lastDelta.TotalTokens,
		}
		if lastDelta.CachedTokens > 0 {
			usage["prompt_tokens_details"] = map[string]any{
				"cached_tokens": lastDelta.CachedTokens,
			}
		}
	}

	resp := map[string]any{
		"id":      reqID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finishReason}},
		"usage":   usage,
	}
	writeJSON(w, resp)
}

// extractDelta parses the {body: <json-string>} SSE wrapper then the inner
// {choices:[{delta:{role,content,tool_calls}}]} shape, mirroring Java.
// It also extracts response_meta.usage for token counts.
func extractDelta(dataLine string) *Delta {
	// Try parsing as headers-style response first (HTTP response wrapper)
	var headersWrapper struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(dataLine), &headersWrapper); err == nil && headersWrapper.Body != "" {
		// Parse the inner body which may contain usage directly
		var directUsage struct {
			Usage struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				Choices []struct {
					Delta struct {
						Role      string `json:"role"`
						Content   string `json:"content"`
						ToolCalls []any  `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			} `json:"usage"`
			Choices []struct {
				Delta struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []any  `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(headersWrapper.Body), &directUsage) == nil {
			promptTokens := directUsage.Usage.PromptTokens
			completionTokens := directUsage.Usage.CompletionTokens
			totalTokens := directUsage.Usage.TotalTokens
			cachedTokens := directUsage.Usage.PromptTokensDetails.CachedTokens

			// Check if there's actual content in choices
			for _, ch := range directUsage.Choices {
				d := ch.Delta
				if d.Role != "" || d.Content != "" || len(d.ToolCalls) > 0 {
					return &Delta{
						Role:             d.Role,
						Content:          d.Content,
						ToolCalls:        d.ToolCalls,
						PromptTokens:     promptTokens,
						CompletionTokens: completionTokens,
						TotalTokens:      totalTokens,
						CachedTokens:     cachedTokens,
					}
				}
			}
			// Return just the usage if no delta content
			return &Delta{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      totalTokens,
				CachedTokens:     cachedTokens,
			}
		}
	}

	// Fallback to original response_meta format
	var wrapper struct {
		Body         string         `json:"body"`
		ResponseMeta map[string]any `json:"response_meta"`
	}
	if json.Unmarshal([]byte(dataLine), &wrapper) != nil || wrapper.Body == "" {
		return nil
	}

	// Extract usage from response_meta
	promptTokens := 0
	completionTokens := 0
	totalTokens := 0
	cachedTokens := 0

	if wrapper.ResponseMeta != nil {
		if usage, ok := wrapper.ResponseMeta["usage"].(map[string]any); ok {
			if pt, ok := usage["prompt_tokens"].(float64); ok {
				promptTokens = int(pt)
			}
			if ct, ok := usage["completion_tokens"].(float64); ok {
				completionTokens = int(ct)
			}
			if tt, ok := usage["total_tokens"].(float64); ok {
				totalTokens = int(tt)
			}
			if ct, ok := usage["cached_tokens"].(float64); ok {
				cachedTokens = int(ct)
			}
		}
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
			return &Delta{
				Role:             d.Role,
				Content:          d.Content,
				ToolCalls:        d.ToolCalls,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      totalTokens,
				CachedTokens:     cachedTokens,
			}
		}
	}
	return &Delta{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		CachedTokens:     cachedTokens,
	}
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
	// Check if headers have already been written (avoid "superfluous WriteHeader" error)
	if wasWritten := w.Header().Get("Content-Type"); wasWritten != "" {
		// Try writing body without header since status code may already be sent
		msg := strings.ReplaceAll(err.Error(), `"`, `\"`)
		body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "qoder_error"}})
		_, _ = w.Write(body)
		return
	}
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
