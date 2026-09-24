package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"qoder2api/internal/cosy"
	"qoder2api/internal/patpool"
)

const chatURL = "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"

type Bridge struct {
	pool     *patpool.Pool
	template []byte
}

func NewBridge(pool *patpool.Pool, template []byte) *Bridge {
	return &Bridge{pool: pool, template: template}
}

// Handler serves the /v1/chat/completions endpoint, mirroring Java handleChat.
func (b *Bridge) Handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		log.Printf("[chat] %s %s -> 405 method not allowed", r.Method, r.URL.Path)
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[chat] request decode failed: %v", err)
		writeErr(w, err)
		return
	}

	// 从会话池取一个会话（round-robin）。
	patID, sess, ok := b.pool.Next()
	if !ok {
		log.Printf("[chat] no enabled PAT session available")
		writeErrJSON(w, http.StatusBadRequest, "no enabled PAT, import first via /v1/pat/import")
		return
	}
	defer func() {
		if err := b.pool.RecordCall(patID); err != nil {
			log.Printf("[pool] record call for pat id=%d failed: %v", patID, err)
		}
	}()

	plan, err := b.planUpstreamRequest(req, sess)
	if err != nil {
		log.Printf("[chat] upstream plan failed: %v", err)
		writeErr(w, err)
		return
	}

	reqID := "chatcmpl-" + strings.ReplaceAll(cosy.NewUUID(), "-", "")[:24]
	created := time.Now().Unix()
	log.Printf("[chat] %s id=%s model=%s stream=%v tools=%v messages=%d pat_id=%d",
		r.RemoteAddr, reqID, plan.model, plan.toolsEnabled, len(req.Tools), len(req.Messages), patID)

	if req.Stream {
		err := b.handleStreamResponse(w, r.Context(), sess, plan.bodyEncoded, plan.extraHeaders, reqID, created, plan.model, plan.toolsEnabled)
		if isAuthError(err) {
			b.pool.MarkDead(patID)
		}
		log.Printf("[chat] id=%s stream done in %s", reqID, time.Since(start))
		return
	}
	err = b.handleNonStreamingResponse(w, r.Context(), sess, plan.bodyEncoded, plan.extraHeaders, reqID, created, plan.model, plan.toolsEnabled)
	if isAuthError(err) {
		b.pool.MarkDead(patID)
	}
	log.Printf("[chat] id=%s non-stream done in %s", reqID, time.Since(start))
}

// isAuthError reports whether err is an upstream auth failure (401/403), which
// means the PAT behind the session is dead and should be disabled.
func isAuthError(err error) bool {
	var ue *cosy.UpstreamError
	if errors.As(err, &ue) {
		return ue.Status == http.StatusUnauthorized || ue.Status == http.StatusForbidden
	}
	return false
}

// writeErrJSON returns a qoder_error JSON body with the given status.
func writeErrJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "qoder_error"}})
	_, _ = w.Write(body)
}

// upstreamPlan is everything derived from the incoming request needed to call
// the Qoder upstream: the encoded body plus the per-request extra headers.
type upstreamPlan struct {
	model        string
	prompt       string
	toolsEnabled bool
	bodyEncoded  []byte
	extraHeaders map[string]string
}

// planUpstreamRequest fills the baseprompt template, injects per-request IDs
// and the converted messages, and encodes the body for the upstream call.
// model is the display name echoed back to the OpenAI client; the upstream key
// is resolved separately and only fed to the Qoder model_config.
func (b *Bridge) planUpstreamRequest(req ChatRequest, sess *cosy.SessionContext) (*upstreamPlan, error) {
	model := req.Model
	if model == "" {
		model = "lite"
	}
	upstreamKey := resolveModelKey(model)
	toolsEnabled := len(req.Tools) > 0

	uuids := []string{cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID(), cosy.NewUUID()}
	bodyBytes, err := FillTemplate(b.template, uuids, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, err
	}

	injectRequestIDs(body, sess.Identity.UserType)
	modelConfig := setModelConfig(body, upstreamKey)
	prompt := setPromptAndBusiness(body, req.Messages)
	body["messages"] = BuildQoderMessages(body["messages"].([]any), req.Messages, prompt, toolsEnabled)
	if toolsEnabled {
		body["tools"] = req.Tools
	}

	return &upstreamPlan{
		model:        model,
		prompt:       prompt,
		toolsEnabled: toolsEnabled,
		bodyEncoded:  []byte(cosy.Encode([]byte(mustJSON(body)))),
		extraHeaders: map[string]string{
			"x-model-key":    upstreamKey,
			"x-model-source": modelConfig["source"].(string),
		},
	}, nil
}

// injectRequestIDs fills the per-request UUIDs and stream flag on the template
// body.
func injectRequestIDs(body map[string]any, aliyunUserType string) {
	nid := cosy.NewUUID()
	body["request_id"] = nid
	body["chat_record_id"] = nid
	body["request_set_id"] = cosy.NewUUID()
	body["session_id"] = cosy.NewUUID()
	body["stream"] = true
	body["aliyun_user_type"] = aliyunUserType
}

// setModelConfig sets the upstream model key on model_config and returns the
// config map for further header use.
func setModelConfig(body map[string]any, upstreamKey string) map[string]any {
	mc := body["model_config"].(map[string]any)
	mc["key"] = upstreamKey
	return mc
}

// setPromptAndBusiness writes the latest user prompt into chat_context and the
// business block, returning the prompt for message conversion.
func setPromptAndBusiness(body map[string]any, messages []any) string {
	prompt := ExtractLatestUserPrompt(messages)
	chatCtx := body["chat_context"].(map[string]any)
	chatCtx["text"].(map[string]any)["text"] = prompt
	chatCtx["extra"].(map[string]any)["originalContent"].(map[string]any)["text"] = prompt

	biz := body["business"].(map[string]any)
	biz["id"] = cosy.NewUUID()
	biz["begin_at"] = time.Now().UnixMilli()
	biz["name"] = truncateRunes(prompt, 30)
	return prompt
}

// truncateRunes truncates s to at most n characters (not bytes).
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
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
		_, _ = w.Write(errorBody(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(errorBody(err))
}

// errorBody builds the qoder_error JSON payload for an error.
func errorBody(err error) []byte {
	msg := strings.ReplaceAll(err.Error(), `"`, `\"`)
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "qoder_error"}})
	return body
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
