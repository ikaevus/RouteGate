package vpnaccounts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/secrets"
)

const subscriptionTokenPrefix = "rgsub_"

func GenerateSubscriptionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return subscriptionTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func HashSubscriptionToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func MaskSubscriptionToken(token string) string {
	return secrets.Mask(token)
}
