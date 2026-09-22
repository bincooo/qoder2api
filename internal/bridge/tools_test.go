package bridge

import (
	"encoding/json"
	"fmt"
	"testing"
)

func pct(md string) []any {
	var a []any
	_ = json.Unmarshal([]byte(md), &a)
	return a
}

func TestParseToolCallsText_Plain(t *testing.T) {
	text := `Tool calls: [{"function":{"name":"f","arguments":"{}"}}]`
	tc := ParseToolCallsText(text)
	if tc == nil || len(tc) != 1 {
		t.Fatalf("got %v", tc)
	}
}

func TestParseToolCallsText_Fenced(t *testing.T) {
	text := "Tool calls:\n```\n[{\"function\":{\"name\":\"g\",\"arguments\":\"{\\\"a\\\":1}\"}}]\n```"
	tc := ParseToolCallsText(text)
	if tc == nil || len(tc) != 1 {
		t.Fatalf("got %v", tc)
	}
}

func TestParseToolCallsText_NoPrefix(t *testing.T) {
	if ParseToolCallsText("hello") != nil {
		t.Fatal("expected nil")
	}
}

func TestNormalizeToolCalls(t *testing.T) {
	in := pct(`[{"function":{"name":"","arguments":""},"foo":1}]`)
	outArr := NormalizeToolCalls(in)
	if len(outArr) != 0 {
		t.Fatalf("blank call should be dropped, got %v", outArr)
	}
	withName := NormalizeToolCalls(pct(`[{"id":"a","type":"function","function":{"name":"x","arguments":"{}"}}]`))
	if len(withName) != 1 {
		t.Fatalf("got %v", withName)
	}
	if withName[0].(map[string]any)["id"] != "a" {
		t.Fatalf("missing id preservation")
	}
}

func TestNormalizeToolArguments(t *testing.T) {
	if NormalizeToolArguments("{}") != "{}" {
		t.Fatal("textual args should pass through")
	}
	if NormalizeToolArguments(map[string]any{"a": 1}) != `{"a":1}` {
		t.Fatalf("object args should be JSON-serialized")
	}
}

func TestSummarizeUnresolvedToolCalls(t *testing.T) {
	names := make([]any, 0, 8)
	for i := 0; i < 8; i++ {
		names = append(names, map[string]any{
			"function": map[string]any{"name": fmt.Sprintf("n%d", i)},
		})
	}
	s := SummarizeUnresolvedToolCalls(names)
	if len(s) == 0 {
		t.Fatal("empty summary")
	}
	if !contains(s, "n0") {
		t.Fatalf("summary should contain first name n0, got %q", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
