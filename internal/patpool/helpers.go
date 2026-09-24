package patpool

import (
	"context"
	"encoding/base64"
	"strings"

	"qoder2api/internal/auth"
)

// helpers.go 提供 DefaultBuilder 所需的机器标识生成与 token 交换辅助函数。

func exchangeJobToken(pat, mid, mtoken, mtype string) (*auth.JobSession, error) {
	return auth.ExchangeJobToken(context.Background(), pat, mid, mtoken, mtype)
}

func base64WithoutPad(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func stripDashes(s string) string { return strings.ReplaceAll(s, "-", "") }

func defaultUserType(t string) string {
	if t == "" {
		return "personal_standard"
	}
	return t
}
