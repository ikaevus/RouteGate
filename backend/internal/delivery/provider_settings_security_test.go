package delivery

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/secrets"
)

func TestProviderSecretEnvelopeIsEncryptedAndProviderBound(t *testing.T) {
	box, err := secrets.NewBox(bytes.Repeat([]byte{0x73}, secrets.KeySize))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	manager := &ProviderSettingsManager{box: box}
	envelope := providerSecretEnvelope{TelegramBotToken: "123456789:super-secret-token"}

	ciphertext, nonce, err := manager.encryptEnvelope("telegram", envelope)
	if err != nil {
		t.Fatalf("encryptEnvelope: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("super-secret-token")) {
		t.Fatal("encrypted provider settings contain plaintext token")
	}

	plaintext, err := box.Open(ciphertext, nonce, providerSecretAAD("telegram", secrets.CurrentVersion))
	if err != nil {
		t.Fatalf("decrypt encrypted settings: %v", err)
	}
	if !bytes.Contains(plaintext, []byte("super-secret-token")) {
		t.Fatal("decrypted provider settings do not contain expected token")
	}
	if _, err := box.Open(ciphertext, nonce, providerSecretAAD("whatsapp", secrets.CurrentVersion)); err == nil {
		t.Fatal("provider-bound ciphertext must not decrypt for another provider")
	}
}

func TestProviderSettingsViewNeverSerializesCredentialValue(t *testing.T) {
	view := ProviderSettingsView{
		Provider:         "telegram",
		Channel:          "telegram",
		Source:           providerSourceManaged,
		Enabled:          true,
		Configured:       true,
		Ready:            true,
		SecretConfigured: true,
		Config:           map[string]any{},
	}

	payload, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	for _, forbidden := range [][]byte{
		[]byte("super-secret-token"),
		[]byte("telegramBotToken"),
		[]byte("whatsAppAccessToken"),
		[]byte("smtpPassword"),
	} {
		if bytes.Contains(payload, forbidden) {
			t.Fatalf("safe provider settings response contains forbidden credential material %q: %s", forbidden, payload)
		}
	}
}
