package cosy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
)

// authPayload mirrors Java authPayloadJson (LinkedHashMap order preserved by struct tags).
type authPayload struct {
	Name               string `json:"name"`
	Aid                string `json:"aid"`
	Uid                string `json:"uid"`
	YxUid              string `json:"yx_uid"`
	OrganizationID     string `json:"organization_id"`
	OrganizationName   string `json:"organization_name"`
	UserType           string `json:"user_type"`
	SecurityOauthToken string `json:"security_oauth_token"`
	RefreshToken       string `json:"refresh_token"`
}

// NewSession mirrors Java newSession: RSA-encrypt a random 16-char tempKey,
// AES-CBC encrypt the auth payload with that tempKey, both base64-encoded.
func NewSession(id AuthIdentity, machineID, machineToken, machineType string) (*SessionContext, error) {
	h := NewUUID()
	h = strings.ReplaceAll(h, "-", "")
	tempKey := []byte(h[:16]) // 16 ASCII hex chars, matches Java .getBytes(US_ASCII)
	cosyKey, err := rsaEncrypt(tempKey)
	if err != nil {
		return nil, err
	}
	infoB64, err := aesEncryptB64(authPayloadJSON(id), tempKey)
	if err != nil {
		return nil, err
	}
	return &SessionContext{
		TempKey:      tempKey,
		CosyKey:      cosyKey,
		Info:         infoB64,
		Identity:     id,
		MachineID:    machineID,
		MachineToken: machineToken,
		MachineType:  machineType,
	}, nil
}

// authPayloadJSON renders the auth payload in the Java field order.
func authPayloadJSON(id AuthIdentity) []byte {
	b, _ := json.Marshal(authPayload{
		Name:               id.Name,
		Aid:                id.Aid,
		Uid:                id.Uid,
		YxUid:              id.YxUid,
		OrganizationID:     id.OrganizationID,
		OrganizationName:   id.OrganizationName,
		UserType:           id.UserType,
		SecurityOauthToken: id.SecurityOauthToken,
		RefreshToken:       id.RefreshToken,
	})
	return b
}

// rsaEncrypt: RSA/ECB/PKCS1Padding encrypt of tempKey with the server pubkey.
func rsaEncrypt(plain []byte) (string, error) {
	block, _ := pem.Decode([]byte(ServerPubkeyPEM))
	if block == nil {
		return "", errors.New("failed to decode server pubkey PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("server key is not RSA")
	}
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// aesEncryptB64: AES/CBC/PKCS5 with tempKey as both key and IV, base64-encoded.
func aesEncryptB64(plain, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:aes.BlockSize] // Java uses the key bytes as the IV too
	padded := pkcs5Pad(plain, aes.BlockSize)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// pkcs5Pad implements PKCS5/PKCS7 (identical) block padding.
func pkcs5Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = byte(padLen)
	}
	return append(data, pad...)
}
