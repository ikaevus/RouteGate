package portal

import (
	"net/http"
	"strings"
)

func requestLocale(r *http.Request) string {
	for _, headerName := range []string{"X-RouteGate-Locale", "Accept-Language"} {
		value := strings.TrimSpace(r.Header.Get(headerName))
		if value == "" {
			continue
		}
		if locale, ok := parseLocale(value); ok {
			return locale
		}
	}

	return "en"
}

func parseLocale(value string) (string, bool) {
	parts := strings.Split(value, ",")
	for _, part := range parts {
		candidate := strings.TrimSpace(strings.Split(part, ";")[0])
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(candidate), "ru") {
			return "ru", true
		}
		if strings.HasPrefix(strings.ToLower(candidate), "en") {
			return "en", true
		}
		if strings.Contains(candidate, "-") {
			base := strings.ToLower(strings.Split(candidate, "-")[0])
			if base == "ru" {
				return "ru", true
			}
			if base == "en" {
				return "en", true
			}
		}
	}

	return "", false
}

func localizedInstructionPlatforms(locale string) []InstructionPlatform {
	if locale == "ru" {
		return []InstructionPlatform{
			{Platform: "ios", DisplayName: "iOS", Description: "Импортируйте ссылку подписки или QR-код на iPhone и iPad."},
			{Platform: "android", DisplayName: "Android", Description: "Импортируйте ссылку подписки или QR-код на Android-устройствах."},
			{Platform: "windows", DisplayName: "Windows", Description: "Импортируйте ссылку подписки в совместимом настольном клиенте."},
			{Platform: "macos", DisplayName: "macOS", Description: "Импортируйте ссылку подписки в совместимом клиенте для macOS."},
			{Platform: "linux", DisplayName: "Linux", Description: "Импортируйте ссылку подписки в совместимом клиенте для Linux."},
		}
	}

	return instructionPlatforms
}

func localizedInstruction(locale, platform string) (DeviceInstruction, bool) {
	if locale == "ru" {
		switch platform {
		case "ios":
			return DeviceInstruction{
				Platform:    "ios",
				DisplayName: "iOS",
				Steps: []string{
					"Установите совместимый клиент VLESS или sing-box.",
					"Откройте свой VPN-профиль RouteGate в пользовательском портале.",
					"Отсканируйте QR-код или скопируйте ссылку подписки.",
					"Импортируйте профиль в клиентское приложение.",
					"Включите импортированный профиль и проверьте подключение.",
				},
				Notes: []string{
					"RouteGate не требует использования одного конкретного коммерческого клиента.",
					"Рекомендации по клиентам должны оставаться настраиваемыми администратором.",
				},
			}, true
		case "android":
			return DeviceInstruction{
				Platform:    "android",
				DisplayName: "Android",
				Steps: []string{
					"Установите совместимый клиент VLESS или sing-box.",
					"Откройте свой VPN-профиль RouteGate в пользовательском портале.",
					"Отсканируйте QR-код или скопируйте ссылку подписки.",
					"Импортируйте подписку в клиентское приложение.",
					"Включите импортированный профиль и проверьте подключение.",
				},
				Notes: []string{
					"Некоторые Android-клиенты могут требовать подтверждения разрешения VPN от системы.",
				},
			}, true
		case "windows":
			return DeviceInstruction{
				Platform:    "windows",
				DisplayName: "Windows",
				Steps: []string{
					"Установите совместимый настольный VPN-клиент.",
					"Скопируйте ссылку подписки из своего профиля RouteGate.",
					"Импортируйте ссылку подписки в клиентское приложение.",
					"Обновите подписку, если клиент поддерживает эту функцию.",
					"Включите импортированный профиль и проверьте подключение.",
				},
				Notes: []string{
					"Рекомендации по настольным клиентам намеренно остаются настраиваемыми.",
				},
			}, true
		case "macos":
			return DeviceInstruction{
				Platform:    "macos",
				DisplayName: "macOS",
				Steps: []string{
					"Установите совместимый VPN-клиент для macOS.",
					"Скопируйте ссылку подписки из своего профиля RouteGate.",
					"Импортируйте ссылку подписки в клиентское приложение.",
					"Включите импортированный профиль и проверьте подключение.",
				},
				Notes: []string{
					"Если клиент запросит разрешение на сетевое расширение, одобрите его в настройках macOS.",
				},
			}, true
		case "linux":
			return DeviceInstruction{
				Platform:    "linux",
				DisplayName: "Linux",
				Steps: []string{
					"Установите совместимый клиент для Linux или рабочий процесс на базе sing-box.",
					"Скопируйте ссылку подписки из своего профиля RouteGate.",
					"Импортируйте или отрисуйте подписку в соответствии с документацией клиента.",
					"Запустите клиент и проверьте подключение.",
				},
				Notes: []string{
					"Настройка Linux может потребовать повышенных привилегий в зависимости от клиента и режима сети.",
				},
			}, true
		}
	}

	instruction, ok := instructionsByPlatform[platform]
	if !ok {
		return DeviceInstruction{}, false
	}
	return instruction, true
}

func localizedSubscriptionWarning(locale string) string {
	if locale == "ru" {
		return "Создайте или обновите ссылку подписки, чтобы получить новый пользовательский URL. RouteGate хранит только хеш токена, поэтому существующие исходные токены нельзя показать повторно."
	}

	return "Generate or refresh the subscription link to reveal a new user-facing URL. RouteGate stores only the token hash, so existing raw tokens cannot be shown again."
}

func localizedSubscriptionInactive(locale string) string {
	if locale == "ru" {
		return "Самообслуживание подпиской недоступно, потому что этот VPN-профиль неактивен."
	}

	return "Subscription self-service is unavailable because this VPN profile is not active."
}

func localizedSubscriptionGenerated(locale string) string {
	if locale == "ru" {
		return "Ссылка подписки сгенерирована. Скопируйте её сейчас; RouteGate не хранит исходные токены подписки."
	}

	return "Subscription link was generated. Copy it now; RouteGate does not store raw subscription tokens."
}

func localizedQRCodeWarning(locale string) string {
	if locale == "ru" {
		return "Создайте или обновите ссылку подписки, чтобы отрисовать новый QR-код. Существующие исходные токены нельзя восстановить из хешей токенов."
	}

	return "Generate or refresh the subscription link to render a new QR code. Existing raw tokens cannot be recovered from token hashes."
}

func localizedQRCodeInactive(locale string) string {
	if locale == "ru" {
		return "Отрисовка QR-кода недоступна, потому что этот VPN-профиль неактивен."
	}

	return "QR rendering is unavailable because this VPN profile is not active."
}
