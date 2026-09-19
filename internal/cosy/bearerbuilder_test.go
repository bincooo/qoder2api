package cosy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestComposeBearer(t *testing.T) {
	got := ComposeBearer("PAY", "SIG")
	want := "Bearer COSY.PAY.SIG"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSignRequest(t *testing.T) {
	// Deterministic concatenation with literal \n separators.
	got := SignRequest("p", "k", "d", "b", "path")
	want := md5Hex("p\nk\nd\nb\npath")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestBuildPayloadB64Order(t *testing.T) {
	got, err := BuildPayloadB64("INFO")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	// Keys must be alphabetical per Java's TreeMap.
	for _, want := range []string{"cosyVersion", "ideVersion", "info", "requestId", "version"} {
		if !strings.Contains(string(raw), `"`+want+`"`) {
			t.Fatalf("missing key %s in %s", want, raw)
		}
	}
}