package delivery

import (
	"errors"
	"strings"
)

func MaskRecipient(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if at := strings.LastIndex(value, "@"); at > 0 && at < len(value)-1 {
		local := []rune(value[:at])
		if len(local) == 0 {
			return "***@" + value[at+1:]
		}
		return string(local[0]) + "***@" + value[at+1:]
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}

func normalizeSafeCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "provider_error"
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return "provider_error"
	}
	return value
}

func normalizeErrorClass(value ErrorClass, fallback ErrorClass) ErrorClass {
	switch value {
	case ErrorClassTransient, ErrorClassPermanent, ErrorClassUncertain:
		return value
	default:
		return fallback
	}
}

func failureFromError(err error, fallbackClass ErrorClass, fallbackCode string) Failure {
	var failure Failure
	if errors.As(err, &failure) {
		failure.Class = normalizeErrorClass(failure.Class, fallbackClass)
		failure.Code = normalizeSafeCode(failure.Code)
		return failure
	}
	return Failure{Class: fallbackClass, Code: normalizeSafeCode(fallbackCode)}
}
