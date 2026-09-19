package bridge

import "strings"

// Delta is one decoded upstream piece, mirroring Java's BridgeDelta record.
type Delta struct {
	Role      string
	Content   string
	ToolCalls []any
}

// isEmpty reports nil or all-three-empty, mirroring Java BridgeDelta.isEmpty.
func (d *Delta) isEmpty() bool {
	if d == nil {
		return true
	}
	return d.Role == "" && d.Content == "" && (d.ToolCalls == nil || len(d.ToolCalls) == 0)
}

// ToolCallAccumulator concatenates tool-call argument fragments by index,
// mirroring Java's ToolCallAccumulator.
type ToolCallAccumulator struct {
	calls []any
}

// Append folds each delta call into the indexed slot, filling placeholders
// up to the target index and concatenating arguments fragments.
func (a *ToolCallAccumulator) Append(deltaCalls []any) {
	for _, d := range deltaCalls {
		dm := mapAny(d)
		index := intIdx(dm["index"])
		for len(a.calls) <= index {
			a.calls = append(a.calls, map[string]any{
				"id": "", "type": "function",
				"function": map[string]any{"name": "", "arguments": ""},
			})
		}
		existing := a.calls[index].(map[string]any)
		if id, ok := dm["id"].(string); ok && id != "" {
			existing["id"] = id
		}
		if typ, ok := dm["type"].(string); ok && typ != "" {
			existing["type"] = typ
		}
		dfn := mapAny(dm["function"])
		efn := existing["function"].(map[string]any)
		if n, ok := dfn["name"].(string); ok && n != "" {
			efn["name"] = n
		}
		if args, ok := dfn["arguments"].(string); ok {
			// concatenate fragments only when the delta carried an index.
			if idx, ok := dm["index"]; ok && idx != nil {
				prev, _ := efn["arguments"].(string)
				efn["arguments"] = prev + args
			} else {
				efn["arguments"] = args
			}
		}
	}
}

func (a *ToolCallAccumulator) IsEmpty() bool  { return len(a.calls) == 0 }
func (a *ToolCallAccumulator) Snapshot() []any { return a.calls }

// intIdx coerces a decoded JSON number to an int (default 0 when absent).
func intIdx(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// StreamAccumulator buffers text that may be a tool-call preamble until it can
// be classified, mirroring Java's StreamAccumulator.
type StreamAccumulator struct {
	emit                    func(role, content string, tc []any)
	toolCallFallbackEnabled bool
	toolCalls               ToolCallAccumulator
	pendingContent          strings.Builder
	pendingRole             string
	emittedChunk            bool
	streamingText           bool
}

func newStreamAccumulator(toolCallFallbackEnabled bool, emit func(role, content string, tc []any)) *StreamAccumulator {
	return &StreamAccumulator{emit: emit, toolCallFallbackEnabled: toolCallFallbackEnabled}
}

// Accept folds one delta into either tool-call accumulation or streamed text,
// mirroring Java accept.
func (s *StreamAccumulator) Accept(d Delta) {
	if d.Role != "" {
		s.pendingRole = d.Role
	}
	if len(d.ToolCalls) > 0 {
		s.discardBufferedToolCallText()
		for _, tc := range withToolCallIndices(d.ToolCalls) {
			s.toolCalls.Append([]any{tc})
		}
		s.emit(s.role(), "", d.ToolCalls)
		return
	}
	if d.Content == "" {
		return
	}
	if !s.toolCallFallbackEnabled || s.streamingText {
		s.streamingText = true
		s.emit(s.role(), d.Content, nil)
		return
	}
	s.pendingContent.WriteString(d.Content)
	if isPotentialToolCallText(s.pendingContent.String()) {
		return
	}
	s.streamingText = true
	s.emitBufferedText()
}

// Flush drains buffered text, re-parsing it as tool calls if possible.
func (s *StreamAccumulator) Flush() {
	if s.pendingContent.Len() == 0 {
		return
	}
	buffered := s.takePending()
	if parsed := ParseToolCallsText(buffered); parsed != nil {
		s.toolCalls.Append(withToolCallIndices(parsed))
		s.emit(s.role(), "", parsed)
		return
	}
	s.streamingText = true
	s.emit(s.role(), buffered, nil)
}

// FinishReason mirrors Java finishReason.
func (s *StreamAccumulator) FinishReason() string {
	if s.toolCalls.IsEmpty() {
		return "stop"
	}
	return "tool_calls"
}

// role returns the pending role (default "assistant") on the first emitted
// chunk and "" afterwards, mirroring Java's emit gate.
func (s *StreamAccumulator) role() string {
	if !s.emittedChunk {
		s.emittedChunk = true
		if s.pendingRole == "" {
			return "assistant"
		}
		return s.pendingRole
	}
	return ""
}

func (s *StreamAccumulator) takePending() string {
	text := s.pendingContent.String()
	s.pendingContent.Reset()
	return text
}

func (s *StreamAccumulator) discardBufferedToolCallText() {
	if s.pendingContent.Len() == 0 {
		return
	}
	buffered := s.takePending()
	if s.toolCallFallbackEnabled && isPotentialToolCallText(buffered) {
		return
	}
	s.streamingText = true
	s.emit(s.role(), buffered, nil)
}

func (s *StreamAccumulator) emitBufferedText() {
	if s.pendingContent.Len() == 0 {
		return
	}
	s.emit(s.role(), s.takePending(), nil)
}

func isPotentialToolCallText(text string) bool {
	return IsPotentialToolCallText(text)
}

// withToolCallIndices injects an index field into each call that lacks one.
func withToolCallIndices(raw []any) []any {
	out := make([]any, 0, len(raw))
	for i, c := range raw {
		cm := mapAny(c)
		if _, ok := cm["index"]; !ok {
			cm["index"] = i
		}
		out = append(out, cm)
	}
	return out
}