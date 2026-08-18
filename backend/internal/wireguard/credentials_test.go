package wireguard

import "testing"

func TestGenerateKeypairReturnsWireGuardKeys(t *testing.T) {
	keypair, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	if err := ValidateKey(keypair.PrivateKey); err != nil {
		t.Fatalf("private key: %v", err)
	}
	if err := ValidateKey(keypair.PublicKey); err != nil {
		t.Fatalf("public key: %v", err)
	}
	if keypair.PrivateKey == keypair.PublicKey {
		t.Fatal("private and public keys must differ")
	}
	derived, err := PublicKeyFromPrivate(keypair.PrivateKey)
	if err != nil {
		t.Fatalf("derive public key: %v", err)
	}
	if derived != keypair.PublicKey {
		t.Fatalf("derived public key = %q, want %q", derived, keypair.PublicKey)
	}
}

func TestNextPeerAddressSkipsUsedAddresses(t *testing.T) {
	got, err := NextPeerAddress("10.66.0.1/24", []string{"10.66.0.2", "10.66.0.3"})
	if err != nil {
		t.Fatalf("next peer address: %v", err)
	}
	if got != "10.66.0.4" {
		t.Fatalf("address = %q, want 10.66.0.4", got)
	}
}

func TestNextPeerAddressRejectsExhaustedPool(t *testing.T) {
	_, err := NextPeerAddress("10.0.0.1/30", []string{"10.0.0.2"})
	if err == nil {
		t.Fatal("expected exhausted pool error")
	}
}
