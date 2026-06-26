package audit

import "github.com/ikaevus/routegate/backend/internal/secrets"

const redactedValue = secrets.RedactedValue

func SanitizeMetadata(metadata map[string]any) map[string]any {
	return secrets.SanitizeMetadata(metadata)
}

func MaskSecret(value string) string {
	return secrets.Mask(value)
}
