package bridge

import (
	"encoding/json"
	"strings"
)

func blankResponseMeta() map[string]any {
	return map[string]any{
		"id": "",
		"usage": map[string]any{
			"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
			"completion_tokens_details": map[string]any{"reasoning_tokens": 0},
			"prompt_tokens_details":     map[string]any{"cached_tokens": 0},
		},
	}
}

func buildUserMessage(text string) map[string]any {
	return map[string]any{
		"role":                        "user",
		"content":                     "",
		"contents":                    []any{map[string]any{"type": "text", "text": text}},
		"response_meta":               blankResponseMeta(),
		"reasoning_content_signature": "",
	}
}

func buildStructuredMessage(role, text string) map[string]any {
	return map[string]any{
		"role":                        role,
		"content":                     text,
		"response_meta":               blankResponseMeta(),
		"reasoning_content_signature": "",
	}
}

func buildAssistantToolCallMessage(text string, toolCalls []any) map[string]any {
	content := text
	if ParseToolCallsText(content) != nil {
		content = ""
	}
	out := buildStructuredMessage("assistant", content)
	out["tool_calls"] = toolCalls
	return out
}

func buildToolMessage(message map[string]any, text string) map[string]any {
	out := buildStructuredMessage("tool", text)
	if n, ok := message["name"].(string); ok {
		out["name"] = n
	}
	if id, ok := message["tool_call_id"].(string); ok {
		out["tool_call_id"] = id
	}
	return out
}

func renderToolResult(message map[string]any, text string) string {
	name, _ := message["name"].(string)
	tcID, _ := message["tool_call_id"].(string)
	var sb strings.Builder
	sb.WriteString("Tool result")
	if name != "" {
		sb.WriteString(" (" + name + ")")
	}
	if tcID != "" {
		sb.WriteString(" [" + tcID + "]")
	}
	if text != "" {
		sb.WriteString(":\n" + text)
	}
	return sb.String()
}

func joinSections(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}

// renderToolCalls mirrors Java renderToolCalls.
func renderToolCalls(toolCalls []any) string {
	return "Tool calls:\n" + stringify(toolCalls)
}

func stringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// extractAnyToolCalls mirrors Java extractAnyToolCalls.
func extractAnyToolCalls(message map[string]any, text string, toolsEnabled bool) []any {
	if !toolsEnabled {
		return nil
	}
	if arr := NormalizeToolCalls(message["tool_calls"]); arr != nil {
		return arr
	}
	return ParseToolCallsText(text)
}

// resolveStructuredToolCalls mirrors Java resolveStructuredToolCalls.
func resolveStructuredToolCalls(message map[string]any, text string, toolsEnabled, allow bool) []any {
	if !toolsEnabled || !allow {
		return nil
	}
	return extractAnyToolCalls(message, text, true)
}

// ConvertIncomingMessage mirrors Java convertIncomingMessage.
func ConvertIncomingMessage(message map[string]any, toolsEnabled, allowStructuredToolCalls bool) map[string]any {
	role := msgRole(message)
	text := msgText(message)
	anyToolCalls := extractAnyToolCalls(message, text, toolsEnabled)
	structured := resolveStructuredToolCalls(message, text, toolsEnabled, allowStructuredToolCalls)

	if role == "assistant" && structured != nil {
		return buildAssistantToolCallMessage(text, structured)
	}
	if role == "assistant" && anyToolCalls != nil && !allowStructuredToolCalls {
		return buildStructuredMessage("assistant", SummarizeUnresolvedToolCalls(anyToolCalls))
	}
	if !toolsEnabled {
		if tc, ok := message["tool_calls"].([]any); ok && len(tc) > 0 {
			text = joinSections(text, renderToolCalls(tc))
		}
	}
	if role == "tool" {
		if toolsEnabled {
			return buildToolMessage(message, text)
		}
		role = "user"
		text = renderToolResult(message, text)
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if role == "user" {
		return buildUserMessage(text)
	}
	return buildStructuredMessage(role, text)
}
