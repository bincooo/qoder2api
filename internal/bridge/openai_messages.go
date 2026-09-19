package bridge

import (
	"encoding/json"
	"strings"
)

func msgRole(m map[string]any) string {
	r, _ := m["role"].(string)
	return r
}

// msgText mirrors Java normalizeMessageText: prefer "content", fall back to
// "contents" when the former is blank.
func msgText(m map[string]any) string {
	t := normalizeContentObj(m["content"])
	if strings.TrimSpace(t) == "" {
		t = normalizeContentObj(m["contents"])
	}
	return t
}

// normalizeContentObj mirrors Java normalizeContent.
func normalizeContentObj(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, it := range c {
			if p := normalizeContentPart(it); strings.TrimSpace(p) != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, "\n\n")
	case map[string]any:
		return normalizeContentPart(c)
	default:
		return ""
	}
}

// normalizeContentPart mirrors Java normalizeContentPart.
func normalizeContentPart(item any) string {
	switch v := item.(type) {
	case string:
		return v
	case map[string]any:
		typ, _ := v["type"].(string)
		if t, ok := v["text"].(string); ok && t != "" {
			return t
		}
		if (typ == "image_url" || typ == "input_image") && strings.TrimSpace(typ) != "" {
			if iu, ok := v["image_url"].(map[string]any); ok {
				if u, ok := iu["url"].(string); ok {
					return "[image] " + u
				}
			}
		}
		if inner, ok := v["content"]; ok {
			return normalizeContentObj(inner)
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// NormalizeContent mirrors Java normalizeContent.
func NormalizeContent(content any) string { return normalizeContentObj(content) }

// ExtractLatestUserPrompt returns the last non-blank user message text.
func ExtractLatestUserPrompt(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if msgRole(m) != "user" {
			continue
		}
		if t := msgText(m); strings.TrimSpace(t) != "" {
			return t
		}
	}
	return ""
}

// hasRole mirrors Java hasRole.
func hasRole(messages []any, role string) bool {
	for _, m := range messages {
		if mm, ok := m.(map[string]any); ok && msgRole(mm) == role {
			return true
		}
	}
	return false
}

// HasResolvedToolResponse mirrors Java hasResolvedToolResponse.
func HasResolvedToolResponse(messages []any, assistantIndex int) bool {
	if assistantIndex < 0 || assistantIndex >= len(messages) {
		return false
	}
	m, ok := messages[assistantIndex].(map[string]any)
	if !ok || msgRole(m) != "assistant" {
		return false
	}
	hasTc := tcArr(m["tool_calls"]) != nil || ParseToolCallsText(msgText(m)) != nil
	if !hasTc {
		return false
	}
	for i := assistantIndex + 1; i < len(messages); i++ {
		mm, ok := messages[i].(map[string]any)
		if !ok {
			return false
		}
		switch msgRole(mm) {
		case "tool":
			return true
		case "assistant", "user", "system":
			return false
		}
	}
	return false
}

func tcArr(v any) []any {
	a, _ := v.([]any)
	return a
}

// BuildQoderMessages mirrors Java buildQoderMessages.
func BuildQoderMessages(templateMessages, incoming []any, prompt string, toolsEnabled bool) []any {
	rebuilt := make([]any, 0, len(incoming)+1)
	keepTemplateSystem := !hasRole(incoming, "system")
	if keepTemplateSystem {
		for _, tm := range templateMessages {
			if mm, ok := tm.(map[string]any); ok && msgRole(mm) == "system" {
				rebuilt = append(rebuilt, mm)
			}
		}
	}
	for i, message := range incoming {
		mm, ok := message.(map[string]any)
		if !ok {
			continue
		}
		allowStructured := toolsEnabled && HasResolvedToolResponse(incoming, i)
		converted := ConvertIncomingMessage(mm, toolsEnabled, allowStructured)
		if converted != nil {
			rebuilt = append(rebuilt, converted)
		}
	}
	if len(rebuilt) == 0 && strings.TrimSpace(prompt) != "" {
		rebuilt = append(rebuilt, buildUserMessage(prompt))
	}
	return rebuilt
}
