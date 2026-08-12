package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	KeySize       = 32
	CurrentVersion = 1
	credentialName = "routegate-master-key"
)

type Box struct {
	aead cipher.AEAD
}

func NewBox(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("routegate master key must contain exactly %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize secret AEAD: %w", err)
	}
	return &Box{aead: aead}, nil
}

func LoadBox(configuredPath string) (*Box, error) {
	path := strings.TrimSpace(configuredPath)
	if credentialsDir := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY")); credentialsDir != "" {
		candidate := filepath.Join(credentialsDir, credentialName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		}
	}
	if path == "" {
		return nil, fmt.Errorf("routegate master key path is not configured")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routegate master key: %w", err)
	}
	return NewBox(key)
}

func (b *Box) Seal(plaintext, additionalData []byte) (ciphertext, nonce []byte, err error) {
	if b == nil || b.aead == nil {
		return nil, nil, fmt.Errorf("secret box is unavailable")
	}
	nonce = make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext = b.aead.Seal(nil, nonce, plaintext, additionalData)
	return ciphertext, nonce, nil
}

func (b *Box) Open(ciphertext, nonce, additionalData []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, fmt.Errorf("secret box is unavailable")
	}
	if len(nonce) != b.aead.NonceSize() {
		return nil, fmt.Errorf("invalid secret nonce")
	}
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}
