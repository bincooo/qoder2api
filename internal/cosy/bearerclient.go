package cosy

import (
	"bufio"
	"context"
	"fmt"
	"log"
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
	log.Printf("[cosy] POST %s body_len=%d", fullURL, len(body))
	req, err := s.buildSignedStreamRequest(ctx, fullURL, body, extraHeaders)
	if err != nil {
		log.Printf("[cosy] build request failed: %v", err)
		return err
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		log.Printf("[cosy] upstream request failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		upstreamErr := readUpstreamError(resp)
		log.Printf("[cosy] upstream non-200 status=%d err=%v", resp.StatusCode, upstreamErr)
		return upstreamErr
	}
	return streamSseLines(resp.Body, onLine)
}

// buildSignedStreamRequest composes the signed Bearer request: cosy-date is
// epoch seconds here, unlike the RFC-1123 date the job-token client signs.
func (s *SessionContext) buildSignedStreamRequest(ctx context.Context, fullURL string, body []byte, extraHeaders map[string]string) (*http.Request, error) {
	u, err := url.Parse(fullURL)
	if err != nil {
		return nil, err
	}
	date := strconv.FormatInt(time.Now().Unix(), 10)
	payloadB64, err := BuildPayloadB64(s.Info)
	if err != nil {
		return nil, err
	}
	sig := SignRequest(payloadB64, s.CosyKey, date, string(body), PathSig(u.Path))
	bearer := ComposeBearer(payloadB64, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	SetCosyAuthHeaders(req, s, date, bearer)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

// readUpstreamError drains a non-200 response body into an UpstreamError.
func readUpstreamError(resp *http.Response) error {
	var buf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
	}
	return &UpstreamError{Status: resp.StatusCode, Body: buf.String()}
}

// streamSseLines reads an SSE body line by line, handing each non-empty line
// (CR-trimmed) to onLine. Max line size is 4 MiB to survive large events.
func streamSseLines(body interface{ Read([]byte) (int, error) }, onLine func(string)) error {
	scanner := bufio.NewScanner(body)
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
