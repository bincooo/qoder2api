package bridge

import "testing"

func TestResolveModelKey_TableHit(t *testing.T) {
	// for every built-in display name, resolution yields the paired upstream key
	for display, want := range modelKeys {
		if got := resolveModelKey(display); got != want {
			t.Errorf("resolveModelKey(%q) = %q, want %q", display, got, want)
		}
	}
}

func TestResolveModelKey_UnknownPassesThrough(t *testing.T) {
	if got := resolveModelKey("custom-model"); got != "custom-model" {
		t.Fatalf("got %q want passthrough", got)
	}
}

func TestListModels_ReturnsAllBuiltIns(t *testing.T) {
	payload := ListModels()
	models := payload["data"].([]map[string]any)
	if len(models) != len(modelKeys) {
		t.Fatalf("expected %d models, got %d", len(modelKeys), len(models))
	}
	if payload["object"] != "list" {
		t.Errorf("object = %v, want list", payload["object"])
	}
	seen := map[string]bool{}
	for _, m := range models {
		id, _ := m["id"].(string)
		if id == "" {
			t.Fatalf("model entry without id: %#v", m)
		}
		if _, ok := modelKeys[id]; !ok {
			t.Fatalf("listed id %q not in mapping table", id)
		}
		if m["object"] != "model" {
			t.Errorf("model %q object = %v, want model", id, m["object"])
		}
		if seen[id] {
			t.Errorf("duplicate model id %q", id)
		}
		seen[id] = true
	}
}

func TestListModels_StableCreated(t *testing.T) {
	first := ListModels()["data"].([]map[string]any)
	second := ListModels()["data"].([]map[string]any)
	if len(first) == 0 {
		t.Fatal("no models built")
	}
	// every entry — and every call — shares one fixed `created` timestamp
	if first[0]["created"] != first[len(first)-1]["created"] {
		t.Errorf("created differs within one list: %v vs %v",
			first[0]["created"], first[len(first)-1]["created"])
	}
	if first[0]["created"] != second[0]["created"] {
		t.Errorf("created changed between calls: %v vs %v",
			first[0]["created"], second[0]["created"])
	}
}
