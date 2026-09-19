package bridge

import (
	"sort"
	"time"
)

// modelKeys maps the OpenAI-facing display name a client sends in `model` to
// the internal Qoder `model_config.key` the upstream expects.
var modelKeys = map[string]string{
	"Qwen3.8-Max":       "qmodel_38max",
	"Qwen3.8-Flash":     "qfmodel",
	"Qwen3.7-Max":       "qmodel_latest",
	"Qwen3.7-Plus":      "qmodel",
	"GLM-5.3":           "gmodel",
	"GLM-5.3-Flash":     "gfmodel",
	"Kimi-K3":           "kmodel_latest",
	"Kimi-K2.8-Preview": "kmodel",
	"DeepSeek-V4-Pro":   "dmodel",
	"DeepSeek-Flash":    "dfmodel",
	"MiniMax-M3":        "mmodel",
}

// modelData is the /v1/models `data` array, built once at startup with a stable
// `created` timestamp (mirroring real OpenAI) and a deterministic ordering.
var modelData []map[string]any

func init() {
	created := time.Now().Unix()
	names := make([]string, 0, len(modelKeys))
	for name := range modelKeys {
		names = append(names, name)
	}
	sort.Strings(names)
	modelData = make([]map[string]any, 0, len(names))
	for _, name := range names {
		modelData = append(modelData, map[string]any{
			"id":       name,
			"object":   "model",
			"created":  created,
			"owned_by": "qoder",
		})
	}
}

// resolveModelKey maps a client-visible model name to the upstream key. Names
// absent from the table pass through unchanged; the empty-name "lite" default
// lives in the chat Handler, which owns the display name sent to the client.
func resolveModelKey(model string) string {
	if key, ok := modelKeys[model]; ok {
		return key
	}
	return model
}

// ListModels returns the full /v1/models payload: one `data` entry per built-in
// model keyed by its display name, so a client can send an id straight back as
// `model`.
func ListModels() map[string]any {
	return map[string]any{
		"object": "list",
		"data":   modelData,
	}
}
