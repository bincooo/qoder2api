package bridge

import (
	"encoding/json"
)

// sseChoiceDelta is the inner {choices:[{delta:{...}}]} shape shared by both
// upstream SSE formats.
type sseChoiceDelta struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []any  `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// usageBody is the inner body's usage block in either upstream format.
type usageBody struct {
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// usageFromMap extracts token counts from the response_meta.usage map shape,
// defaulting every field to zero when absent.
func usageFromMap(usage map[string]any) UsageStats {
	u := UsageStats{}
	if usage == nil {
		return u
	}
	if pt, ok := usage["prompt_tokens"].(float64); ok {
		u.PromptTokens = int(pt)
	}
	if ct, ok := usage["completion_tokens"].(float64); ok {
		u.CompletionTokens = int(ct)
	}
	if tt, ok := usage["total_tokens"].(float64); ok {
		u.TotalTokens = int(tt)
	}
	if ct, ok := usage["cached_tokens"].(float64); ok {
		u.CachedTokens = int(ct)
	}
	return u
}

// usageFromBody extracts token counts from the typed usage block.
func usageFromBody(u usageBody) UsageStats {
	return UsageStats{
		PromptTokens:     u.Usage.PromptTokens,
		CompletionTokens: u.Usage.CompletionTokens,
		TotalTokens:      u.Usage.TotalTokens,
		CachedTokens:     u.Usage.PromptTokensDetails.CachedTokens,
	}
}

// deltaWithUsage folds the choice deltas and usage into one Delta, preferring
// the first choice that carries actual content and returning a usage-only
// delta when none do.
func deltaWithUsage(inner sseChoiceDelta, usage UsageStats) *Delta {
	for _, ch := range inner.Choices {
		d := ch.Delta
		if d.Role != "" || d.Content != "" || len(d.ToolCalls) > 0 {
			return &Delta{
				Role:             d.Role,
				Content:          d.Content,
				ToolCalls:        d.ToolCalls,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				CachedTokens:     usage.CachedTokens,
			}
		}
	}
	return &Delta{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CachedTokens:     usage.CachedTokens,
	}
}

// extractDelta parses the {body: <json-string>} SSE wrapper then the inner
// {choices:[{delta:{role,content,tool_calls}}]} shape, mirroring Java.
// It also extracts usage for token counts, from either the direct usage block
// or response_meta.usage depending on which wrapper shape the line has.
func extractDelta(dataLine string) *Delta {
	if d := extractDeltaFromDirectUsage(dataLine); d != nil {
		return d
	}
	return extractDeltaFromResponseMeta(dataLine)
}

// extractDeltaFromDirectUsage handles the headers-style wrapper whose body
// carries a usage block alongside choices.
func extractDeltaFromDirectUsage(dataLine string) *Delta {
	var headersWrapper struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(dataLine), &headersWrapper); err != nil || headersWrapper.Body == "" {
		return nil
	}
	var inner struct {
		usageBody
		sseChoiceDelta
	}
	if err := json.Unmarshal([]byte(headersWrapper.Body), &inner); err != nil {
		return nil
	}
	return deltaWithUsage(inner.sseChoiceDelta, usageFromBody(inner.usageBody))
}

// extractDeltaFromResponseMeta handles the original wrapper whose response_meta
// carries the usage map.
func extractDeltaFromResponseMeta(dataLine string) *Delta {
	var wrapper struct {
		Body         string         `json:"body"`
		ResponseMeta map[string]any `json:"response_meta"`
	}
	if err := json.Unmarshal([]byte(dataLine), &wrapper); err != nil || wrapper.Body == "" {
		return nil
	}
	var inner sseChoiceDelta
	if err := json.Unmarshal([]byte(wrapper.Body), &inner); err != nil {
		return nil
	}
	return deltaWithUsage(inner, usageFromResponseMeta(wrapper.ResponseMeta))
}

// usageFromResponseMeta safely extracts the usage map out of response_meta.
func usageFromResponseMeta(meta map[string]any) UsageStats {
	if meta == nil {
		return UsageStats{}
	}
	usage, _ := meta["usage"].(map[string]any)
	return usageFromMap(usage)
}
