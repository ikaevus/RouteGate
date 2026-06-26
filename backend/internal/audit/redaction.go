package audit

import "strings"

const redactedValue = "[redacted]"

func SanitizeMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}

	sanitized := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if isSensitiveMetadataKey(key) {
			sanitized[key] = redactedValue
			continue
		}
		sanitized[key] = sanitizeValue(value)
	}
	return sanitized
}

func MaskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "..."
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return SanitizeMetadata(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, sanitizeValue(item))
		}
		return items
	default:
		return value
	}
}

func isSensitiveMetadataKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "preview") || strings.Contains(normalized, "masked") || strings.Contains(normalized, "last4") {
		return false
	}

	sensitiveFragments := []string{
		"password",
		"private_key",
		"privatekey",
		"secret",
		"credential",
		"jwt",
		"access_token",
		"refresh_token",
		"session_token",
		"registration_token",
		"registrationtoken",
		"subscription_token",
		"subscriptiontoken",
		"subscription_url",
		"subscriptionurl",
		"token_hash",
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
