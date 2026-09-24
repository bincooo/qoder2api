package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qoder2api/internal/auth"
	"qoder2api/internal/bridge"
	"qoder2api/internal/cosy"
	"qoder2api/internal/patpool"
	"qoder2api/internal/patstore"
)

// writeJSON writes v as an application/json response, mirroring the bridge
// package's writer the server also uses.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

// --- /v1/login 设备登录端点 ---

// loginSessionTTL 是登录会话在 loginSessions 中的保留时长：超过后若未被
// /v1/login/callback 取走，则自动销毁。
const loginSessionTTL = 60 * time.Second

// loginSessions 保存进行中的设备登录会话，按 sessionID 索引。
var (
	loginMu       sync.Mutex
	loginSessions = map[string]*loginSession{}
)

type loginSession struct {
	loginID   string
	createdAt time.Time // 会话创建时间，用于 60 秒 TTL 自动清理
	expiresAt time.Time // 上游授权超时时间（CompleteLogin 的 pollTimeout）
	outDir    string    // auth JSON 保存目录，文件名固定为 {session_id}.json
}

// startLoginSessionJanitor 启动后台清理协程：每 10 秒扫描一次 loginSessions，
// 删除创建超过 loginSessionTTL（60 秒）仍未被 callback 消费的会话。
func startLoginSessionJanitor() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			loginMu.Lock()
			for id, sess := range loginSessions {
				if now.Sub(sess.createdAt) >= loginSessionTTL {
					delete(loginSessions, id)
					log.Printf("[login] session expired and cleaned id=%s", id)
				}
			}
			loginMu.Unlock()
		}
	}()
}

// sessionAuthJSONPath 返回会话对应的 auth JSON 文件路径：{outDir}/{sessionID}.json。
// outDir 为空时使用默认目录（qoder2api 配置目录的目录部分）。
func sessionAuthJSONPath(sessionID, outDir string) string {
	if outDir == "" {
		outDir = filepath.Dir("./auths/")
	}
	return filepath.Join(outDir, sessionID+".json")
}

// readSavedAuthJSON 尝试读取已保存的 {session_id}.json。文件存在且内容合法
// 则返回其字节，否则返回 nil（调用方应回退到内存会话或报错）。
func readSavedAuthJSON(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil
	}
	return data
}

// handleLoginStart 创建设备登录会话并返回验证 URL 与 session_id。
func handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrJSON(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	resp, err := auth.StartLogin()
	if err != nil {
		log.Printf("[login] start failed: %v", err)
		writeErrJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	// auth JSON 保存目录：?output= 指定，默认为 qoder2api 配置目录。
	// 文件名固定为 {session_id}.json，不固定为 auth.json。
	outDir := r.URL.Query().Get("output")
	sessionID := cosy.NewUUID()
	outPath := sessionAuthJSONPath(sessionID, outDir)
	if dir := filepath.Dir(outPath); dir != "" {
		if err = os.MkdirAll(dir, 0700); err != nil {
			writeErrJSON(w, http.StatusInternalServerError, fmt.Sprintf("创建目录失败: %v", err))
			return
		}
	}

	// 本地映射 session_id → auth 包的 pending 登录信息
	loginMu.Lock()
	loginSessions[sessionID] = &loginSession{
		loginID:   resp.LoginID,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		outDir:    outDir,
	}
	loginMu.Unlock()

	log.Printf("[login] session started id=%s output=%s", sessionID, outPath)
	writeJSON(w, map[string]any{
		"session_id":       sessionID,
		"verification_uri": resp.VerificationURI,
		"expires_in":       resp.ExpiresIn,
	})
}

// handleLoginCallback 通过 session_id 完成认证并返回 auth JSON。
func handleLoginCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrJSON(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeErrJSON(w, http.StatusBadRequest, "session_id is required")
		return
	}

	outDir := r.URL.Query().Get("output")

	loginMu.Lock()
	sess := loginSessions[sessionID]
	delete(loginSessions, sessionID) // 单次有效，取走后即销毁
	loginMu.Unlock()

	outPath := sessionAuthJSONPath(sessionID, outDir)

	if sess == nil {
		// 内存会话已被清理（TTL 到期或服务重启）：到保存目录中查找
		// {session_id}.json，若授权早已完成并落盘，则直接返回该文件。
		if data := readSavedAuthJSON(outPath); data != nil {
			log.Printf("[login] session %s not in memory, returning saved %s", sessionID, outPath)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		writeErrJSON(w, http.StatusGone, "login session not found or expired, call /v1/login/start first")
		return
	}
	if now := time.Now(); now.Sub(sess.createdAt) >= loginSessionTTL || now.After(sess.expiresAt) {
		// 会话在内存中已超时，但若此前已完成授权并落盘，仍可返回保存的文件。
		if data := readSavedAuthJSON(outPath); data != nil {
			log.Printf("[login] session %s expired in memory, returning saved %s", sessionID, outPath)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		writeErrJSON(w, http.StatusGone, "login session expired, call /v1/login/start again")
		return
	}

	result, err := auth.CompleteLogin(sess.loginID)
	if err != nil {
		log.Printf("[login] callback failed: %v", err)
		writeErrJSON(w, http.StatusGone, err.Error())
		return
	}

	if err = auth.SaveAuthJSON(outPath, result); err != nil {
		log.Printf("[login] save auth json failed: %v", err)
		writeErrJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	name, _ := result.UserStatus["name"].(string)
	email, _ := result.UserStatus["email"].(string)
	log.Printf("[login] authorized user=%s (%s) saved=%s", name, email, outPath)

	writeJSON(w, result)
}

// writeErrJSON returns a qoder_error JSON body with the given status.
func writeErrJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "qoder_error"}})
	_, _ = w.Write(body)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func base64WithoutPad(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func first18(s string) string { return strings.ReplaceAll(s, "-", "")[:18] }

func defaultUserType(t string) string {
	if t == "" {
		return "personal_standard"
	}
	return t
}

func main() {
	host := envOr("QODER_HOST", "127.0.0.1")
	port := envOr("QODER_PORT", "8963")
	addr := host + ":" + port

	dbPath := envOr("QODER_DB", "./qoder.db")
	store, err := patstore.Open(dbPath)
	if err != nil {
		log.Fatalf("[patstore] open %s: %v", dbPath, err)
	}
	defer store.Close()

	// 向后兼容：QODER_PAT 环境变量若设置且库中无任何启用 PAT，自动导入。
	if pat := os.Getenv("QODER_PAT"); pat != "" {
		enabled, _ := store.ListEnabled()
		if len(enabled) == 0 {
			if imported, _, err := store.Import([]string{pat}); err == nil && imported > 0 {
				log.Printf("[patstore] imported QODER_PAT from env")
			}
		}
	}

	pool, err := patpool.NewPool(store, patpool.DefaultBuilder)
	if err != nil {
		log.Fatalf("[patpool] init: %v", err)
	}
	log.Printf("[patpool] %d session(s) ready", pool.Size())

	template := bridge.LoadEmbeddedTemplate()
	b := bridge.NewBridge(pool, template)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", b.Handler)
	mux.HandleFunc("/v1/models", listModels)
	mux.HandleFunc("/v1/login/start", handleLoginStart)
	mux.HandleFunc("/v1/login/callback", handleLoginCallback)
	mux.HandleFunc("/v1/pat/import", handlePatImport(store, pool))
	startLoginSessionJanitor()

	log.Printf("[bridge] listening http://%s/v1/chat/completions (login: /v1/login/start, pat: /v1/pat/import)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// handlePatImport 接收 PAT 数组并导入到 SQLite，导入后刷新会话池。
// POST /v1/pat/import  Body: ["pat1","pat2",...]  → {"imported":n,"duplicated":n}
func handlePatImport(store *patstore.Store, pool *patpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErrJSON(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var pats []string
		if err := json.NewDecoder(r.Body).Decode(&pats); err != nil {
			writeErrJSON(w, http.StatusBadRequest, "body must be a JSON array of PAT strings")
			return
		}
		imported, duplicated, err := store.Import(pats)
		if err != nil {
			log.Printf("[patstore] import failed: %v", err)
			writeErrJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := pool.Refresh(); err != nil {
			log.Printf("[patpool] refresh after import failed: %v", err)
		}
		log.Printf("[patstore] imported=%d duplicated=%d pool_size=%d", imported, duplicated, pool.Size())
		writeJSON(w, map[string]any{"imported": imported, "duplicated": duplicated})
	}
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
