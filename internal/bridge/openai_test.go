package bridge

import "testing"

func msgs(arr ...map[string]any) []any {
	out := make([]any, 0, len(arr))
	for _, m := range arr {
		out = append(out, m)
	}
	return out
}

func TestExtractLatestUserPrompt_LastUserWins(t *testing.T) {
	m := msgs(
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "assistant", "content": "middle"},
		map[string]any{"role": "user", "content": "last"},
	)
	if got := ExtractLatestUserPrompt(m); got != "last" {
		t.Fatalf("got %q want last", got)
	}
}

func TestExtractLatestUserPrompt_BlankSkipped(t *testing.T) {
	m := msgs(
		map[string]any{"role": "user", "content": ""},
		map[string]any{"role": "user", "content": "keeping"},
	)
	if got := ExtractLatestUserPrompt(m); got != "keeping" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractLatestUserPrompt_BlankContentsSkipped(t *testing.T) {
	// content blank, contents carries the text (mirrors Java normalizeMessageText)
	m := msgs(
		map[string]any{"role": "user", "content": "", "contents": []any{map[string]any{"type": "text", "text": "from-contents"}}},
		map[string]any{"role": "assistant", "content": "nope"},
	)
	if got := ExtractLatestUserPrompt(m); got != "from-contents" {
		t.Fatalf("got %q want from-contents", got)
	}
}

func TestNormalizeContent_ArrayAndImage(t *testing.T) {
	got := NormalizeContent([]any{
		map[string]any{"type": "text", "text": "hi"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://x/y.png"}},
	})
	if got != "hi\n\n[image] http://x/y.png" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestNormalizeContent_PlainText(t *testing.T) {
	if got := NormalizeContent("plain"); got != "plain" {
		t.Fatalf("got %q want plain", got)
	}
}

func TestNormalizeContent_InputImage(t *testing.T) {
	got := NormalizeContent([]any{
		map[string]any{"type": "input_image", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
	})
	if got != "[image] data:image/png;base64,abc" {
		t.Fatalf("got %q", got)
	}
}

func TestHasResolvedToolResponse(t *testing.T) {
	// assistant with tool_calls followed by a tool message -> resolved
	m := msgs(
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "t1", "type": "function", "function": map[string]any{"name": "Bash", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "t1", "content": "42"},
	)
	if !HasResolvedToolResponse(m, 0) {
		t.Fatal("expected resolved tool response")
	}
	// assistant with tool_calls but next is another assistant -> unresolved
	m2 := msgs(
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "t2", "type": "function", "function": map[string]any{"name": "Bash", "arguments": "{}"}},
		}},
		map[string]any{"role": "assistant", "content": "following"},
	)
	if HasResolvedToolResponse(m2, 0) {
		t.Fatal("expected unresolved tool response")
	}
}

func TestBuildUserMessageShape(t *testing.T) {
	got := buildUserMessage("hello")
	role, _ := got["role"].(string)
	if role != "user" {
		t.Fatalf("role = %q", role)
	}
	if c, _ := got["content"].(string); c != "" {
		t.Fatalf("content should be empty, got %q", c)
	}
	contents, ok := got["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %#v", got["contents"])
	}
	rm, ok := got["response_meta"].(map[string]any)
	if !ok {
		t.Fatalf("response_meta missing")
	}
	if _, ok := rm["usage"]; !ok {
		t.Fatalf("response_meta.usage missing")
	}
	if _, ok := got["reasoning_content_signature"]; !ok {
		t.Fatalf("reasoning_content_signature missing")
	}
}
