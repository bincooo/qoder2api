package cosy

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
)

// ServerPubkeyPEM is the Qoder server RSA public key (verbatim from Java BearerBuilder).
const ServerPubkeyPEM = "-----BEGIN PUBLIC KEY-----\n" +
	"MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc\n" +
	"4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l\n" +
	"6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17\n" +
	"XcW+ML9FoCI6AOvOzwIDAQAB\n" +
	"-----END PUBLIC KEY-----"

type AuthIdentity struct {
	Name, Aid, Uid, YxUid, OrganizationID, OrganizationName, UserType, SecurityOauthToken, RefreshToken string
}

type SessionContext struct {
	TempKey      []byte
	CosyKey      string
	Info         string
	Identity     AuthIdentity
	MachineID    string
	MachineToken string
	MachineType  string
}

// BuildPayloadB64 produces the alphabetically-sorted JSON payload, base64-encoded.
// Mirrors Java buildPayloadB64, which sorts the map with a TreeMap (alphabetical keys).
func BuildPayloadB64(info string) (string, error) {
	m := map[string]string{
		"cosyVersion": "0.1.43",
		"ideVersion":  "",
		"info":        info,
		"requestId":   NewUUID(),
		"version":     "v1",
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		sb.Write(kb)
		sb.WriteByte(':')
		sb.Write(vb)
	}
	sb.WriteString("}")
	return base64.StdEncoding.EncodeToString([]byte(sb.String())), nil
}

// SignRequest mirrors Java signRequest: md5(payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + pathWithoutAlgo).
func SignRequest(payloadB64, cosyKey, cosyDate, body, pathWithoutAlgo string) string {
	return md5Hex(payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + pathWithoutAlgo)
}

// ComposeBearer returns the authorization header value.
func ComposeBearer(payloadB64, sig string) string {
	return "Bearer COSY." + payloadB64 + "." + sig
}