package agents

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns the SHA-256 digest used for persisted agent and
// registration-token lookups. Repositories accept only this hashed value.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
