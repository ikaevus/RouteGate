package secrets

import (
	"bytes"
	"testing"
)

func TestBoxRoundTripBindsAdditionalData(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, KeySize)
	box, err := NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}

	plaintext := []byte(`{"accessToken":"secret-value"}`)
	aad := []byte("routegate:delivery-provider-settings:whatsapp:v1")
	ciphertext, nonce, err := box.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("secret-value")) {
		t.Fatal("ciphertext contains plaintext secret")
	}

	decrypted, err := box.Open(ciphertext, nonce, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round trip mismatch: got %q", decrypted)
	}
	if _, err := box.Open(ciphertext, nonce, []byte("wrong-provider")); err == nil {
		t.Fatal("expected AAD mismatch to fail")
	}
}

func TestNewBoxRejectsWrongKeySize(t *testing.T) {
	if _, err := NewBox(make([]byte, KeySize-1)); err == nil {
		t.Fatal("expected invalid key size to fail")
	}
}
