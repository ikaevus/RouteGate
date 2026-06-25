package servers

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateRealityKeypairReturnsBase64URLX25519Keys(t *testing.T) {
	keypair, err := GenerateRealityKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	if keypair.PrivateKey == "" || keypair.PublicKey == "" {
		t.Fatalf("generated keypair has empty fields: %+v", keypair)
	}
	if strings.Contains(keypair.PrivateKey, "=") || strings.Contains(keypair.PublicKey, "=") {
		t.Fatalf("Reality keys must use unpadded base64url encoding: %+v", keypair)
	}

	privateBytes, err := base64.RawURLEncoding.DecodeString(keypair.PrivateKey)
	if err != nil {
		t.Fatalf("decode private key: %v", err)
	}
	publicBytes, err := base64.RawURLEncoding.DecodeString(keypair.PublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if len(privateBytes) != 32 || len(publicBytes) != 32 {
		t.Fatalf("decoded key lengths = private %d public %d, want 32/32", len(privateBytes), len(publicBytes))
	}
}
