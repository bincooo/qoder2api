package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"qoder2api/internal/cosy"
)

const jobTokenURL = "https://center.qoder.sh/algo/api/v3/user/jobToken?Encode=1"

type JobSession struct {
	Name, ID, UserType, SecurityOauthToken, RefreshToken string
}

// innerTokenInfo mirrors the inner payload of the Java exchangeJobToken request.
type innerTokenInfo struct {
	PersonalToken      string   `json:"personalToken"`
	SecurityOauthToken string   `json:"securityOauthToken"`
	RefreshToken       string   `json:"refreshToken"`
	NeedRefresh        bool     `json:"needRefresh"`
	AuthInfo           struct{} `json:"authInfo"`
}

// outerBody mirrors the outer {payload, encodeVersion} wrapper.
type outerBody struct {
	Payload       string `json:"payload"`
	EncodeVersion string `json:"encodeVersion"`
}

// BuildExchangeBody returns the Qoder-custom-encoded request body for the
// jobToken exchange. Verbatim to Java SignatureApiClient.exchangeJobToken.
func BuildExchangeBody(personalToken string) ([]byte, error) {
	inner := innerTokenInfo{
		PersonalToken: personalToken,
		NeedRefresh:   false,
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return nil, err
	}
	outer := outerBody{
		Payload:       string(innerJSON),
		EncodeVersion: "1",
	}
	outerJSON, err := json.Marshal(outer)
	if err != nil {
		return nil, err
	}
	return []byte(cosy.Encode(outerJSON)), nil
}

// ExchangeJobToken POSTs to the qoder center and returns the session identity.
func ExchangeJobToken(ctx context.Context, personalToken, machineID, machineToken, machineType string) (*JobSession, error) {
	body, err := BuildExchangeBody(personalToken)
	if err != nil {
		return nil, err
	}
	date := cosy.CurrentDate()
	sig := cosy.Sign(date)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jobTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-machinetoken", machineToken)
	req.Header.Set("cosy-machinetype", machineType)
	req.Header.Set("login-version", "v2")
	req.Header.Set("appcode", cosy.APPCODE)
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("cosy-version", "0.1.43")
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("date", date)
	req.Header.Set("signature", sig)
	req.Header.Set("cosy-machineid", machineID)
	req.Header.Set("user-agent", "Go-http-client/2.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("jobToken HTTP %d body=%s", resp.StatusCode, b)
	}
	var raw struct {
		Name               string `json:"name"`
		ID                 string `json:"id"`
		UserType           string `json:"userType"`
		SecurityOauthToken string `json:"securityOauthToken"`
		RefreshToken       string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return &JobSession{
		Name:               raw.Name,
		ID:                 raw.ID,
		UserType:           raw.UserType,
		SecurityOauthToken: raw.SecurityOauthToken,
		RefreshToken:       raw.RefreshToken,
	}, nil
}