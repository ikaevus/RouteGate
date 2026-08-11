package secrets

import "strings"

const RedactedValue = "[redacted]"

type Classification string

const (
	ClassificationPublic    Classification = "public"
	ClassificationSensitive Classification = "sensitive"
	ClassificationSecret    Classification = "secret"
)

func Mask(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "..."
	}
	for _, prefix := range []string{"rgsub_", "rg_reg_", "rg_agent_"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix)+8 {
			body := strings.TrimPrefix(value, prefix)
			return prefix + body[:4] + "..." + body[len(body)-4:]
		}
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func Redact(value any) string {
	return RedactedValue
}

func ClassifyKey(key string) Classification {
	normalized := normalizeKey(key)
	if normalized == "" {
		return ClassificationPublic
	}
	if strings.Contains(normalized, "preview") || strings.Contains(normalized, "masked") || strings.Contains(normalized, "last4") {
		return ClassificationSensitive
	}
	if strings.Contains(normalized, "public_key") || strings.Contains(normalized, "publickey") {
		return ClassificationPublic
	}

	secretFragments := []string{
		"password",
		"private_key",
		"privatekey",
		"secret",
		"credential",
		"jwt",
		"api_key",
		"apikey",
		"bot_token",
		"bottoken",
		"provider_token",
		"providertoken",
		"whatsapp_token",
		"whatsapptoken",
		"access_token",
		"refresh_token",
		"session_token",
		"registration_token",
		"registrationtoken",
		"subscription_token",
		"subscriptiontoken",
		"subscription_url",
		"subscriptionurl",
		"access_url",
		"accessurl",
		"connect_url",
		"connecturl",
		"vless_uri",
		"vlessuri",
		"vless_link",
		"vlesslink",
		"qr_payload",
		"qrpayload",
		"message_body",
		"messagebody",
		"html_body",
		"htmlbody",
		"delivery_payload",
		"deliverypayload",
		"raw_response",
		"rawresponse",
		"raw_error",
		"rawerror",
		"agent_token",
		"agenttoken",
		"token_hash",
		"hash",
	}
	for _, fragment := range secretFragments {
		if strings.Contains(normalized, fragment) {
			return ClassificationSecret
		}
	}
	return ClassificationPublic
}

func IsSecretKey(key string) bool {
	return ClassifyKey(key) == ClassificationSecret
}

func SanitizeMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}

	sanitized := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if IsSecretKey(key) {
			sanitized[key] = RedactedValue
			continue
		}
		sanitized[key] = sanitizeValue(value)
	}
	return sanitized
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

func normalizeKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}
