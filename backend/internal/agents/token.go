package agents

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const tokenRandomBytes = 32

// GenerateRegistrationToken creates a one-time server registration credential.
// Callers must persist only HashToken's result, never the returned raw token.
func GenerateRegistrationToken() (string, error) {
	random := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "rg_reg_" + base64.RawURLEncoding.EncodeToString(random), nil
}

// GenerateAgentToken creates a permanent agent bearer credential. Callers must
// persist only HashToken's result and return the raw token only at registration.
func GenerateAgentToken() (string, error) {
	random := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "rg_agent_" + base64.RawURLEncoding.EncodeToString(random), nil
}

// HashToken returns the SHA-256 digest used for persisted agent and
// registration-token lookups. Repositories accept only this hashed value.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func MaskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "..."
	}
	for _, prefix := range []string{"rg_reg_", "rg_agent_"} {
		if strings.HasPrefix(token, prefix) && len(token) > len(prefix)+8 {
			body := strings.TrimPrefix(token, prefix)
			return prefix + body[:4] + "..." + body[len(body)-4:]
		}
	}
	return token[:4] + "..." + token[len(token)-4:]
}
