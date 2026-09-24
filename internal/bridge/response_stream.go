package bridge

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"qoder2api/internal/cosy"
)

// streamResult carries the state gathered while consuming an upstream SSE stream.
type streamResult struct {
	lastDelta *Delta
}

// dataLineFromSse returns the JSON payload of an SSE line, or "" when the line
// is not a data line.
func dataLineFromSse(line string) string {
	if !strings.HasPrefix(line, "data:") {
		return ""
	}
	return strings.TrimSpace(line[5:])
}

// handleStreamResponse runs the streaming path of /v1/chat/completions: it
// posts to the upstream, re-emits each delta as an OpenAI SSE chunk, and
// finishes with a terminal chunk carrying usage and finish_reason.
func (b *Bridge) handleStreamResponse(
	w http.ResponseWriter,
	ctx context.Context,
	sess *cosy.SessionContext,
	bodyEncoded []byte,
	extra map[string]string,
	reqID string,
	created int64,
	model string,
	toolsEnabled bool,
) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)

	res := &streamResult{}
	acc := NewStreamAccumulator(toolsEnabled, func(role, content string, tc []any) {
		writeSseChunk(w, reqID, created, model, role, content, tc)
		if fl != nil {
			fl.Flush()
		}
	})

	err := sess.SignedPostStream(ctx, chatURL, bodyEncoded, extra, func(line string) {
		payload := dataLineFromSse(line)
		if payload == "" {
			return
		}
		delta := extractDelta(payload)
		if delta == nil {
			return
		}
		res.lastDelta = delta
		if delta.isEmpty() {
			return
		}
		acc.Accept(*delta)
	})
	if err != nil {
		log.Printf("[chat] id=%s upstream stream error: %v", reqID, err)
		writeErr(w, err)
		return err
	}
	acc.Flush()
	writeTerminalChunk(w, reqID, created, model, acc.FinishReason(), usageFromDelta(res.lastDelta))
	log.Printf("[chat] id=%s stream finished reason=%s", reqID, acc.FinishReason())
	return nil
}

// usageFromDelta returns the OpenAI-style usage map for a delta, or the zeroed
// usage when the delta carries no token counts.
func usageFromDelta(d *Delta) map[string]any {
	if d == nil || !d.hasTokens() {
		return map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
	}
	return UsageStats{
		PromptTokens:     d.PromptTokens,
		CompletionTokens: d.CompletionTokens,
		TotalTokens:      d.TotalTokens,
		CachedTokens:     d.CachedTokens,
	}.UsageMap()
}

// writeTerminalChunk emits the final empty-delta chunk (finish_reason + usage)
// followed by the [DONE] sentinel, mirroring Java's stream epilogue.
func writeTerminalChunk(w http.ResponseWriter, reqID string, created int64, model, finishReason string, usage map[string]any) {
	chunk := makeChunk(reqID, created, model)
	choice := chunk["choices"].([]any)[0].(map[string]any)
	choice["finish_reason"] = finishReason
	choice["delta"] = map[string]any{}
	choice["usage"] = usage
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
	fmt.Fprint(w, "data: [DONE]\n\n")
}
