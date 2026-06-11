package agents

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const registrationTokenRandomBytes = 32

// GenerateRegistrationToken creates a one-time server registration credential.
// Callers must persist only HashToken's result, never the returned raw token.
func GenerateRegistrationToken() (string, error) {
	random := make([]byte, registrationTokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "rg_reg_" + base64.RawURLEncoding.EncodeToString(random), nil
}

// HashToken returns the SHA-256 digest used for persisted agent and
// registration-token lookups. Repositories accept only this hashed value.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
