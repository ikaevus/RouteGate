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
			{Platform: "ios", DisplayName: "iOS", Description: "Используйте прямой QR-профиль в приложении, совместимом с протоколом, указанным в RouteGate."},
			{Platform: "android", DisplayName: "Android", Description: "Используйте прямой QR-профиль в приложении, совместимом с протоколом, указанным в RouteGate."},
			{Platform: "windows", DisplayName: "Windows", Description: "Импортируйте прямые данные подключения в совместимый настольный клиент."},
			{Platform: "macos", DisplayName: "macOS", Description: "Импортируйте прямые данные подключения в совместимый клиент для macOS."},
			{Platform: "linux", DisplayName: "Linux", Description: "Импортируйте прямые данные подключения в совместимый клиент для Linux."},
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
					"Откройте свой VPN-профиль RouteGate и посмотрите указанный для него протокол.",
					"Установите приложение, совместимое с этим протоколом. Для MTProto используйте Telegram.",
					"Откройте прямой QR-код подключения в RouteGate и отсканируйте его совместимым приложением либо скопируйте содержимое QR для ручного импорта.",
					"Подтвердите импортированный профиль или настройки прокси в приложении.",
					"Включите подключение и проверьте его работу.",
				},
				Notes: []string{
					"RouteGate поддерживает несколько протоколов; совместимость клиента определяется протоколом, указанным в профиле.",
					"URL подписки RouteGate — отдельный расширенный формат и не заменяет прямой QR-код подключения.",
				},
			}, true
		case "android":
			return DeviceInstruction{
				Platform:    "android",
				DisplayName: "Android",
				Steps: []string{
					"Откройте свой VPN-профиль RouteGate и посмотрите указанный для него протокол.",
					"Установите приложение, совместимое с этим протоколом. Для MTProto используйте Telegram.",
					"Откройте прямой QR-код подключения в RouteGate и отсканируйте его совместимым приложением либо скопируйте содержимое QR для ручного импорта.",
					"Подтвердите импортированный профиль или настройки прокси в приложении.",
					"Включите подключение и проверьте его работу.",
				},
				Notes: []string{
					"Некоторые Android-клиенты могут требовать подтверждения разрешения VPN от системы.",
					"URL подписки RouteGate — отдельный расширенный формат и не заменяет прямой QR-код подключения.",
				},
			}, true
		case "windows":
			return DeviceInstruction{
				Platform:    "windows",
				DisplayName: "Windows",
				Steps: []string{
					"Откройте свой VPN-профиль RouteGate и посмотрите указанный для него протокол.",
					"Установите настольный клиент, совместимый с этим протоколом. Для MTProto используйте Telegram Desktop.",
					"Откройте прямой QR-код подключения и отсканируйте его, если клиент поддерживает QR-импорт, либо скопируйте содержимое QR и импортируйте его вручную.",
					"Подтвердите импортированный профиль или настройки прокси.",
					"Включите подключение и проверьте его работу.",
				},
				Notes: []string{
					"Рекомендации по клиентам намеренно не привязаны к одному коммерческому приложению.",
					"URL подписки RouteGate — отдельный расширенный формат и не заменяет прямые данные подключения клиента.",
				},
			}, true
		case "macos":
			return DeviceInstruction{
				Platform:    "macos",
				DisplayName: "macOS",
				Steps: []string{
					"Откройте свой VPN-профиль RouteGate и посмотрите указанный для него протокол.",
					"Установите клиент для macOS, совместимый с этим протоколом. Для MTProto используйте Telegram.",
					"Откройте прямой QR-код подключения и отсканируйте его, если это поддерживается, либо скопируйте содержимое для ручного импорта.",
					"Подтвердите импортированный профиль или настройки прокси.",
					"Включите подключение и проверьте его работу.",
				},
				Notes: []string{
					"Если клиент запросит разрешение на сетевое расширение, одобрите его в настройках macOS.",
					"URL подписки RouteGate — отдельный расширенный формат и не заменяет прямые данные подключения клиента.",
				},
			}, true
		case "linux":
			return DeviceInstruction{
				Platform:    "linux",
				DisplayName: "Linux",
				Steps: []string{
					"Откройте свой VPN-профиль RouteGate и посмотрите указанный для него протокол.",
					"Установите клиент для Linux, совместимый с этим протоколом. Для MTProto используйте клиент Telegram.",
					"Откройте прямой QR-код подключения и скопируйте его содержимое или конфигурацию в клиент.",
					"Импортируйте или сохраните профиль согласно документации клиента.",
					"Запустите подключение и проверьте его работу.",
				},
				Notes: []string{
					"Настройка Linux может потребовать повышенных привилегий в зависимости от клиента и режима сети.",
					"URL подписки RouteGate — отдельный расширенный формат и не заменяет прямые данные подключения клиента.",
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
		return "Расширенный URL подписки RouteGate можно создать или обновить отдельно. Для обычного подключения используйте прямой QR-код профиля; исходные токены подписки RouteGate хранит только в виде хеша."
	}

	return "The advanced RouteGate subscription URL can be generated or refreshed separately. For normal setup, use the profile's direct connection QR code; RouteGate stores subscription tokens only as hashes."
}

func localizedSubscriptionInactive(locale string) string {
	if locale == "ru" {
		return "Расширенная подписка недоступна, потому что этот VPN-профиль неактивен."
	}

	return "The advanced subscription is unavailable because this VPN profile is not active."
}

func localizedSubscriptionGenerated(locale string) string {
	if locale == "ru" {
		return "Расширенный URL подписки создан. Скопируйте его сейчас, если он нужен вашему рабочему процессу; RouteGate не хранит исходные токены подписки."
	}

	return "The advanced subscription URL was generated. Copy it now if your workflow needs it; RouteGate does not store raw subscription tokens."
}

func localizedQRCodeWarning(locale string) string {
	if locale == "ru" {
		return "Этот QR-код содержит прямые данные подключения для протокола вашего профиля. Считайте его учётными данными доступа и не передавайте посторонним."
	}

	return "This QR code contains the direct connection material for your profile protocol. Treat it as an access credential and do not share it with unauthorized people."
}

func localizedQRCodeNotReady(locale string) string {
	if locale == "ru" {
		return "Прямые данные подключения пока не готовы. Проверьте назначение узла и настройку протокола или обратитесь к администратору."
	}

	return "Direct connection material is not ready yet. Check the node assignment and protocol setup or contact the administrator."
}

func localizedQRCodeInactive(locale string) string {
	if locale == "ru" {
		return "Прямой QR-код подключения недоступен, потому что этот VPN-профиль неактивен."
	}

	return "The direct connection QR code is unavailable because this VPN profile is not active."
}
