package cosy

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const customAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
const stdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
const customPad = '$'

func b64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// customFor maps a standard-base64 byte to its custom counterpart.
// Returns 0 if c is not in the expected alphabet.
func customFor(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return customAlphabet[c-'A']
	}
	if c >= 'a' && c <= 'z' {
		return customAlphabet[26+c-'a']
	}
	if c >= '0' && c <= '9' {
		return customAlphabet[52+c-'0']
	}
	switch c {
	case '+':
		return customAlphabet[62]
	case '/':
		return customAlphabet[63]
	case '=':
		return '$'
	}
	return 0
}

var inv = buildInv()

func buildInv() map[byte]byte {
	m := make(map[byte]byte, 65)
	for i := 0; i < 64; i++ {
		m[customAlphabet[i]] = stdAlphabet[i]
	}
	m['$'] = '='
	return m
}

// Encode is the Qoder custom-base64: standard Base64, a 3-chunk rearrangement,
// then an alphabet remap with '$' for '='. Matches Java QoderEncoding.encode.
func Encode(plain []byte) string {
	std := b64Std(plain)
	n := len(std)
	if n == 0 {
		return ""
	}
	a := n / 3
	rearranged := std[n-a:] + std[a:n-a] + std[:a]
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		c := rearranged[i]
		m := customFor(c)
		if m == 0 {
			panic("char out of alphabet: " + string(c))
		}
		sb.WriteByte(m)
	}
	return sb.String()
}

// Decode inverts Encode.
func Decode(encoded string) ([]byte, error) {
	n := len(encoded)
	if n == 0 {
		return []byte{}, nil
	}
	rev := make([]byte, n)
	for i := 0; i < n; i++ {
		v, ok := inv[encoded[i]]
		if !ok {
			return nil, fmt.Errorf("char out of custom alphabet: %q", encoded[i])
		}
		rev[i] = v
	}
	mapped := string(rev)
	a := n / 3
	norm := mapped[n-a:] + mapped[a:n-a] + mapped[:a]
	return base64.StdEncoding.DecodeString(norm)
}