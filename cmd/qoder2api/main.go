package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"qoder2api/internal/auth"
	"qoder2api/internal/bridge"
	"qoder2api/internal/cosy"
)

// writeJSON writes v as an application/json response, mirroring the bridge
// package's writer the server also uses.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func currentCtx() context.Context { return context.Background() }

func base64WithoutPad(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func first18(s string) string { return strings.ReplaceAll(s, "-", "")[:18] }

func defaultUserType(t string) string {
	if t == "" {
		return "personal_standard"
	}
	return t
}

func main() {
	pat := os.Getenv("QODER_PAT")
	if pat == "" {
		log.Fatal("Token required!")
	}
	host := envOr("QODER_HOST", "127.0.0.1")
	port := envOr("QODER_PORT", "8963")
	addr := host + ":" + port

	mid := cosy.NewUUID()
	mtoken := base64WithoutPad([]byte((cosy.NewUUID() + cosy.NewUUID())[:50]))
	mtype := first18(cosy.NewUUID())

	js, err := auth.ExchangeJobToken(currentCtx(), pat, mid, mtoken, mtype)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("[bridge] session for %s (%s)\n", js.Name, js.ID)

	identity := cosy.AuthIdentity{
		Name:               js.Name,
		Aid:                js.ID,
		Uid:                js.ID,
		YxUid:              "",
		OrganizationID:     "",
		OrganizationName:   "",
		UserType:           defaultUserType(js.UserType),
		SecurityOauthToken: js.SecurityOauthToken,
		RefreshToken:       js.RefreshToken,
	}
	sess, err := cosy.NewSession(identity, mid, mtoken, mtype)
	if err != nil {
		log.Fatal(err)
	}

	template, err := bridge.LoadEmbeddedTemplate()
	if err != nil {
		log.Fatal(err)
	}

	b := bridge.NewBridge(sess, template)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", b.Handler)
	mux.HandleFunc("/v1/models", listModels)

	log.Printf("[bridge] listening http://%s/v1/chat/completions", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// listModels serves GET /v1/models: the OpenAI list payload built by bridge,
// whose ids are the display names clients can send straight back as `model`.
func listModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, bridge.ListModels())
}
