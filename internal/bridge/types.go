package bridge

import (
	"context"

	"qoder2api/internal/cosy"
)

// ChatRequest mirrors the incoming OpenAI chat completion request body.
type ChatRequest struct {
	Stream   bool   `json:"stream"`
	Model    string `json:"model"`
	Messages []any  `json:"messages"`
	Tools    []any  `json:"tools"`
}

// ChatRequestContext carries everything needed to build the upstream Qoder body.
type ChatRequestContext struct {
	Request      ChatRequest
	Model        string // display name echoed back to the OpenAI client
	UpstreamKey  string // resolved Qoder model_config.key
	Prompt       string // latest user prompt text
	ToolsEnabled bool
}

// qoderRequestBody is a typed view of the filled baseprompt template.
type qoderRequestBody struct {
	body        map[string]any
	modelConfig map[string]any
	business    map[string]any
	chatContext map[string]any
}

// UsageStats aggregates token counts from an upstream delta.
type UsageStats struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}

// UsageMap returns the OpenAI-style usage object (with prompt_tokens_details).
func (u UsageStats) UsageMap() map[string]any {
	return map[string]any{
		"prompt_tokens":     u.PromptTokens,
		"completion_tokens": u.CompletionTokens,
		"total_tokens":      u.TotalTokens,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": u.CachedTokens,
		},
	}
}

// SessionLike abstracts the subset of *cosy.SessionContext the bridge needs.
// Kept as an interface for testability (Dependency Inversion).
type SessionLike interface {
	SignedPostStream(ctx context.Context, fullURL string, body []byte, extraHeaders map[string]string, onLine func(string)) error
}

var _ SessionLike = (*cosy.SessionContext)(nil)
