package bridge

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// nonStreamResult carries the state gathered while consuming the upstream
// stream on the non-streaming path.
type nonStreamResult struct {
	full      strings.Builder
	acc       ToolCallAccumulator
	lastDelta *Delta
}

// handleNonStreamingResponse runs the aggregate path of /v1/chat/completions:
// it consumes the whole upstream stream, folds text and tool calls into a
// single OpenAI chat.completion JSON body.
func (b *Bridge) handleNonStreamingResponse(
	w http.ResponseWriter,
	ctx context.Context,
	bodyEncoded []byte,
	extra map[string]string,
	reqID string,
	created int64,
	model string,
	toolsEnabled bool,
) {
	res := &nonStreamResult{}
	err := b.sess.SignedPostStream(ctx, chatURL, bodyEncoded, extra, func(line string) {
		payload := dataLineFromSse(line)
		if payload == "" {
			return
		}
		delta := extractDelta(payload)
		if delta == nil {
			return
		}
		res.lastDelta = delta
		if delta.Content != "" {
			res.full.WriteString(delta.Content)
		}
		if len(delta.ToolCalls) > 0 {
			res.acc.Append(delta.ToolCalls)
		}
	})
	if err != nil {
		log.Printf("[chat] id=%s upstream stream error: %v", reqID, err)
		writeErr(w, err)
		return
	}

	finishReason := res.finishReason(toolsEnabled)
	resp := map[string]any{
		"id":      reqID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       res.assistantMessage(toolsEnabled),
			"finish_reason": finishReason,
		}},
		"usage": usageFromDelta(res.lastDelta),
	}
	writeJSON(w, resp)
	log.Printf("[chat] id=%s non-stream finished reason=%s content_len=%d tool_calls=%d",
		reqID, finishReason, res.full.Len(), len(res.acc.Snapshot()))
}

// assistantMessage folds accumulated text and tool calls into the OpenAI
// assistant message, re-parsing text as tool calls when tools are enabled but
// none arrived structurally (mirroring Java lines 132-183).
func (r *nonStreamResult) assistantMessage(toolsEnabled bool) map[string]any {
	fallbackToolCalls := []any(nil)
	if r.acc.IsEmpty() && toolsEnabled {
		fallbackToolCalls = ParseToolCallsText(r.full.String())
	}

	msg := map[string]any{"role": "assistant"}
	switch {
	case fallbackToolCalls != nil:
		msg["content"] = nil
		msg["tool_calls"] = fallbackToolCalls
	case r.full.Len() == 0 && !r.acc.IsEmpty():
		msg["content"] = nil
	default:
		msg["content"] = r.full.String()
	}
	if !r.acc.IsEmpty() {
		msg["tool_calls"] = r.acc.Snapshot()
	}
	return msg
}

// finishReason reports "tool_calls" when any tool calls (structured or
// re-parsed) were produced, "stop" otherwise.
func (r *nonStreamResult) finishReason(toolsEnabled bool) string {
	if !r.acc.IsEmpty() {
		return "tool_calls"
	}
	if toolsEnabled && r.acc.IsEmpty() && ParseToolCallsText(r.full.String()) != nil {
		return "tool_calls"
	}
	return "stop"
}
