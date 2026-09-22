package cosy

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// sharedHTTPClient is a global HTTP client for streaming with proper connection pooling.
// Created once at startup to enable HTTP/2 stream reuse and prevent "stream ID 5" errors
// after extended runtime.
var sharedHTTPClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	},
}

// PathSig strips a leading "/algo" from the request path, matching Java.
func PathSig(rawPath string) string {
	p := rawPath
	if strings.HasPrefix(p, "/algo") {
		p = p[len("/algo"):]
	}
	return p
}

// SignedPostStream posts body to fullURL with all cosy headers and streams
// each non-empty SSE line to onLine. body must already be Qoder-encoded.
func (s *SessionContext) SignedPostStream(ctx context.Context, fullURL string, body []byte, extraHeaders map[string]string, onLine func(string)) error {
	u, err := url.Parse(fullURL)
	if err != nil {
		return err
	}
	date := strconv.FormatInt(time.Now().Unix(), 10)
	payloadB64, err := BuildPayloadB64(s.Info)
	if err != nil {
		return err
	}
	sig := SignRequest(payloadB64, s.CosyKey, date, string(body), PathSig(u.Path))
	bearer := ComposeBearer(payloadB64, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	// Mirror Java openStreamLines headers verbatim.
	setHeader := func(k, v string) { req.Header.Set(k, v) }
	setHeader("cosy-data-policy", "AGREE")
	setHeader("content-type", "application/json")
	setHeader("cosy-machinetype", s.MachineType)
	setHeader("cosy-clienttype", "5")
	setHeader("cosy-date", date)
	setHeader("cosy-user", s.Identity.Uid)
	setHeader("cosy-key", s.CosyKey)
	setHeader("cache-control", "no-cache")
	setHeader("accept", "text/event-stream")
	setHeader("cosy-clientip", "169.254.198.161")
	setHeader("authorization", bearer)
	setHeader("accept-encoding", "identity")
	setHeader("cosy-version", "0.1.43")
	setHeader("cosy-machineid", s.MachineID)
	setHeader("cosy-machinetoken", s.MachineToken)
	setHeader("login-version", "v2")
	setHeader("user-agent", "Go-http-client/2.0")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := sharedHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var buf strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			buf.WriteString(scanner.Text())
		}
		return &UpstreamError{Status: resp.StatusCode, Body: buf.String()}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		onLine(line)
	}
	return scanner.Err()
}

// UpstreamError reports a non-200 upstream response.
type UpstreamError struct {
	Status int
	Body   string
}

func (e *UpstreamError) Error() string { return fmt.Sprintf("HTTP %d %s", e.Status, e.Body) }
