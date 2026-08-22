package delivery

import (
	"html"
	"strings"
)

const (
	defaultBrandName = "RouteGate"
	defaultBrandURL  = "https://routegate.org"
	defaultLogoURL   = "https://routegate.org/routegate-symbol.svg"
)

func DefaultDeliveryBranding(locale string) DeliveryBranding {
	locale = strings.ToLower(strings.TrimSpace(locale))
	footer := "Powered by RouteGate\nSelf-hosted VPN Management Platform\nroutegate.org"
	if locale == "ru" {
		footer = "Отправлено через RouteGate\nПлатформа управления self-hosted VPN\nroutegate.org"
	}
	return DeliveryBranding{
		BrandName:    defaultBrandName,
		WebsiteURL:   defaultBrandURL,
		LogoURL:      defaultLogoURL,
		FooterText:   footer,
		ShowBranding: true,
	}
}

func normalizeDeliveryBranding(locale string, branding DeliveryBranding) DeliveryBranding {
	if !branding.ShowBranding && strings.TrimSpace(branding.BrandName) == "" && strings.TrimSpace(branding.FooterText) == "" {
		return DefaultDeliveryBranding(locale)
	}
	branding.BrandName = strings.TrimSpace(branding.BrandName)
	branding.WebsiteURL = strings.TrimSpace(branding.WebsiteURL)
	branding.LogoURL = strings.TrimSpace(branding.LogoURL)
	branding.FooterText = strings.TrimSpace(branding.FooterText)
	return branding
}

func appendDeliveryBranding(text, htmlBody string, branding DeliveryBranding) (string, string) {
	if !branding.ShowBranding {
		return strings.TrimSpace(text), strings.TrimSpace(htmlBody)
	}
	text = strings.TrimSpace(text)
	if branding.FooterText != "" {
		if text != "" {
			text += "\n\n"
		}
		text += branding.FooterText
	}

	htmlBody = strings.TrimSpace(htmlBody)
	if htmlBody == "" {
		return text, htmlBody
	}
	var footer strings.Builder
	footer.WriteString(`<div style="margin-top:24px;padding-top:16px;border-top:1px solid #e5e7eb;color:#6b7280;font-size:12px;line-height:1.5">`)
	if branding.LogoURL != "" {
		footer.WriteString(`<p style="margin:0 0 8px"><img src="`)
		footer.WriteString(html.EscapeString(branding.LogoURL))
		footer.WriteString(`" alt="`)
		footer.WriteString(html.EscapeString(branding.BrandName))
		footer.WriteString(`" width="28" height="28" style="display:block;border:0"></p>`)
	}
	for index, line := range strings.Split(branding.FooterText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if index == 0 && branding.WebsiteURL != "" {
			footer.WriteString(`<div><a href="`)
			footer.WriteString(html.EscapeString(branding.WebsiteURL))
			footer.WriteString(`" style="color:#4b5563;text-decoration:none">`)
			footer.WriteString(html.EscapeString(line))
			footer.WriteString(`</a></div>`)
			continue
		}
		footer.WriteString(`<div>`)
		footer.WriteString(html.EscapeString(line))
		footer.WriteString(`</div>`)
	}
	footer.WriteString(`</div>`)
	htmlBody += footer.String()
	return text, htmlBody
}
