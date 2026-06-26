package vpnaccounts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
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
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, subscriptionTokenPrefix) && len(token) > len(subscriptionTokenPrefix)+8 {
		body := strings.TrimPrefix(token, subscriptionTokenPrefix)
		return subscriptionTokenPrefix + body[:4] + "..." + body[len(body)-4:]
	}
	if len(token) <= 8 {
		return "..."
	}
	return token[:4] + "..." + token[len(token)-4:]
}
