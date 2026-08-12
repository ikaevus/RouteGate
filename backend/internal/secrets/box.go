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
	KeySize        = 32
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
	path := resolveKeyPath(configuredPath)
	if path == "" {
		return nil, fmt.Errorf("routegate master key path is not configured")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routegate master key: %w", err)
	}
	return NewBox(key)
}

func CreateKeyFile(configuredPath string) error {
	path := strings.TrimSpace(configuredPath)
	if path == "" {
		return fmt.Errorf("routegate master key path is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create routegate secret state directory: %w", err)
	}
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generate routegate master key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create routegate master key: %w", err)
	}
	removeOnFailure := true
	defer func() {
		_ = file.Close()
		if removeOnFailure {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(key); err != nil {
		return fmt.Errorf("write routegate master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync routegate master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close routegate master key: %w", err)
	}
	removeOnFailure = false
	return nil
}

func resolveKeyPath(configuredPath string) string {
	path := strings.TrimSpace(configuredPath)
	if credentialsDir := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY")); credentialsDir != "" {
		candidate := filepath.Join(credentialsDir, credentialName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		}
	}
	return path
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
