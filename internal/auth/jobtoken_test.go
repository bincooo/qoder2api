package auth

import (
	"strings"
	"testing"

	"qoder2api/internal/cosy"
)

func TestBuildExchangeBody(t *testing.T) {
	b, err := BuildExchangeBody("MY_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	// Qoder custom-encodes the outer {payload, encodeVersion} JSON.
	// Round-trip through cosy.Decode and confirm it contains "personalToken".
	decoded, err := cosy.Decode(string(b))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !strings.Contains(string(decoded), "personalToken") {
		t.Fatalf("decoded body missing personalToken: %s", decoded)
	}
	if !strings.Contains(string(decoded), "encodeVersion") {
		t.Fatalf("decoded body missing encodeVersion: %s", decoded)
	}
}
