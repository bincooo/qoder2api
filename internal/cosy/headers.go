package cosy

import "net/http"

// Shared COSY request constants (values mirrored verbatim from the Java client).
const (
	cosyVersion       = "0.1.43"
	cosyClientType    = "5"
	cosyClientIP      = "169.254.198.161"
	cosyDataPolicy    = "AGREE"
	loginVersion      = "v2"
	userAgent         = "Go-http-client/2.0"
	contentTypeJSON   = "application/json"
	acceptJSON        = "application/json"
	acceptEventStream = "text/event-stream"
	acceptEncoding    = "identity"
	cacheControl      = "no-cache"
)

// SetCosyAuthHeaders sets the fixed COSY authorization headers shared by the
// streaming (Bearer) client, matching Java's openStreamLines header set.
// The caller supplies the epoch-second cosy-date and the composed Bearer value.
func SetCosyAuthHeaders(req *http.Request, s *SessionContext, date, bearer string) {
	req.Header.Set("cosy-data-policy", cosyDataPolicy)
	req.Header.Set("content-type", contentTypeJSON)
	req.Header.Set("cosy-machinetype", s.MachineType)
	req.Header.Set("cosy-clienttype", cosyClientType)
	req.Header.Set("cosy-date", date)
	req.Header.Set("cosy-user", s.Identity.Uid)
	req.Header.Set("cosy-key", s.CosyKey)
	req.Header.Set("cache-control", cacheControl)
	req.Header.Set("accept", acceptEventStream)
	req.Header.Set("cosy-clientip", cosyClientIP)
	req.Header.Set("authorization", bearer)
	req.Header.Set("accept-encoding", acceptEncoding)
	req.Header.Set("cosy-version", cosyVersion)
	req.Header.Set("cosy-machineid", s.MachineID)
	req.Header.Set("cosy-machinetoken", s.MachineToken)
	req.Header.Set("login-version", loginVersion)
	req.Header.Set("user-agent", userAgent)
}

// SetJobTokenHeaders sets the fixed headers for the job-token exchange against
// center.qoder.sh, matching Java's SignatureApiClient header set. The caller
// supplies the RFC-1123 date and its MD5 signature.
func SetJobTokenHeaders(req *http.Request, machineID, machineToken, machineType, date, sig string) {
	req.Header.Set("content-type", contentTypeJSON)
	req.Header.Set("cosy-machinetoken", machineToken)
	req.Header.Set("cosy-machinetype", machineType)
	req.Header.Set("login-version", loginVersion)
	req.Header.Set("appcode", APPCODE)
	req.Header.Set("accept", acceptJSON)
	req.Header.Set("accept-encoding", acceptEncoding)
	req.Header.Set("cosy-version", cosyVersion)
	req.Header.Set("cosy-clienttype", cosyClientType)
	req.Header.Set("date", date)
	req.Header.Set("signature", sig)
	req.Header.Set("cosy-machineid", machineID)
	req.Header.Set("user-agent", userAgent)
}
