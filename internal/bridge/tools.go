package bridge

import (
	"encoding/json"
	"strconv"
	"strings"
)

const toolCallsPrefix = "Tool calls:"

// ParseToolCallsText extracts the tool-call array from a "Tool calls:"-prefixed
// text payload (optionally wrapped in ``` fences). nil if not present/parseable.
func ParseToolCallsText(text string) []any {
	p := strings.TrimSpace(text)
	if !strings.HasPrefix(p, toolCallsPrefix) {
		return nil
	}
	payload := strings.TrimSpace(p[len(toolCallsPrefix):])
	if strings.HasPrefix(payload, "```") && strings.HasSuffix(payload, "```") {
		nl := strings.IndexByte(payload, '\n')
		if nl >= 0 {
			payload = strings.TrimSpace(payload[nl+1 : len(payload)-3])
		}
	}
	if !strings.HasPrefix(payload, "[") {
		return nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(payload), &arr); err != nil {
		return nil
	}
	return NormalizeToolCalls(arr)
}

// NormalizeToolCalls returns calls with defaults filled and blanks dropped.
func NormalizeToolCalls(raw any) []any {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(arr))
	for _, c := range arr {
		cm, _ := c.(map[string]any)
		fn := mapAny(cm["function"])
		name, _ := fn["name"].(string)
		args := NormalizeToolArguments(fn["arguments"])
		if name == "" && args == "" {
			continue
		}
		call := map[string]any{}
		if id, _ := cm["id"].(string); id != "" {
			call["id"] = id
		} else {
			call["id"] = ""
		}
		if typ, _ := cm["type"].(string); typ != "" {
			call["type"] = typ
		} else {
			call["type"] = "function"
		}
		call["function"] = map[string]any{"name": name, "arguments": args}
		out = append(out, call)
	}
	return out
}

func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// NormalizeToolArguments passes strings through and JSON-serializes objects,
// matching Java normalizeToolArguments.
func NormalizeToolArguments(args any) string {
	switch v := args.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// SummarizeUnresolvedToolCalls mirrors Java summarizeUnresolvedToolCalls.
func SummarizeUnresolvedToolCalls(tc []any) string {
	var sb strings.Builder
	sb.WriteString("Previously planned but unexecuted tool calls")
	limit := len(tc)
	if limit > 6 {
		limit = 6
	}
	names := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		name, _ := mapAny(mapAny(tc[i])["function"])["name"].(string)
		if name == "" {
			name = "unknown"
		}
		names = append(names, name)
	}
	if len(names) > 0 {
		sb.WriteString(": ")
		sb.WriteString(strings.Join(names, ", "))
	}
	if len(tc) > limit {
		sb.WriteString(" and ")
		sb.WriteString(strconv.Itoa(len(tc) - limit))
		sb.WriteString(" more")
	}
	sb.WriteString(".")
	return sb.String()
}

// IsPotentialToolCallText mirrors Java isPotentialToolCallText: a trimmed-empty
// candidate or a candidate that overlaps the "Tool calls:" prefix.
func IsPotentialToolCallText(text string) bool {
	candidate := strings.TrimLeft(text, " \t\n\r\f\v")
	if candidate == "" {
		return true
	}
	return strings.HasPrefix(toolCallsPrefix, candidate) || strings.HasPrefix(candidate, toolCallsPrefix)
}