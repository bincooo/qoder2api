package bridge

import (
	"strings"
	"testing"
)

// ---- ToolCallAccumulator ----
func TestToolCallAccumulator_ConcatenatesArguments(t *testing.T) {
	acc := &ToolCallAccumulator{}
	acc.Append([]any{map[string]any{"index": 0, "id": "a", "function": map[string]any{"name": "f", "arguments": `{"x":`}}})
	acc.Append([]any{map[string]any{"index": 0, "function": map[string]any{"arguments": "1}"}}})
	if acc.IsEmpty() {
		t.Fatal("should not be empty")
	}
	snap := acc.Snapshot()
	c := snap[0].(map[string]any)
	fn := c["function"].(map[string]any)
	if fn["arguments"] != `{"x":1}` {
		t.Fatalf("arguments not concatenated: %v", fn["arguments"])
	}
	if c["id"] != "a" || c["type"] != "function" {
		t.Fatalf("missing id/type defaults: %#v", c)
	}
}

// ---- StreamAccumulator ----
func TestStreamAccumulator_BuffersToolCallPreamble(t *testing.T) {
	var raw string
	sa := newStreamAccumulator(true, func(role, content string, tc []any) {
		raw += content
	})
	sa.Accept(Delta{Content: "Tool "})
	sa.Accept(Delta{Content: "calls: hello"})
	sa.Flush()
	if !strings.Contains(raw, "Tool calls: hello") {
		t.Fatalf("buffered text not flushed: %q", raw)
	}
}

func TestStreamAccumulator_ReparsesToolCall(t *testing.T) {
	var raw string
	accused := false
	sa := newStreamAccumulator(true, func(role, content string, tc []any) {
		if tc != nil {
			accused = true
		}
		raw += content
	})
	sa.Accept(Delta{Content: `Tool calls: [{"function":{"name":"f","arguments":"{}"}}]`})
	sa.Flush()
	if !accused {
		t.Fatal("expected tool-call emission after flush")
	}
}

func TestStreamAccumulator_FinishReason(t *testing.T) {
	sa := newStreamAccumulator(true, func(role, content string, tc []any) {})
	if sa.FinishReason() != "stop" {
		t.Fatal("empty acc should finish stop")
	}
	sa.Accept(Delta{ToolCalls: []any{map[string]any{"index": 0, "function": map[string]any{"name": "f", "arguments": "{}"}}}})
	if sa.FinishReason() != "tool_calls" {
		t.Fatal("with tool calls should finish tool_calls")
	}
}