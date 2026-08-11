package delivery

import (
	"bytes"
	"embed"
	"io/fs"
	"path"
	"strings"
	"text/template"
)

//go:embed templates/*/*.tmpl
var embeddedTemplates embed.FS

type Renderer struct {
	files fs.FS
}

func NewRenderer() *Renderer {
	return &Renderer{files: embeddedTemplates}
}

func newRendererWithFS(files fs.FS) *Renderer {
	return &Renderer{files: files}
}

func (r *Renderer) Render(templateKey, locale string, data TemplateData) (Message, error) {
	templateKey = strings.TrimSpace(templateKey)
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		locale = "en"
	}
	if !validTemplateKey(templateKey) || !validLocale(locale) {
		return Message{}, Failure{Class: ErrorClassPermanent, Code: "template_not_found"}
	}

	content, err := fs.ReadFile(r.files, path.Join("templates", locale, templateKey+".tmpl"))
	if err != nil {
		return Message{}, Failure{Class: ErrorClassPermanent, Code: "template_not_found"}
	}
	parsed, err := template.New(templateKey).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return Message{}, Failure{Class: ErrorClassPermanent, Code: "template_invalid"}
	}

	subject, err := executeTemplateBlock(parsed, "subject", data)
	if err != nil {
		return Message{}, err
	}
	text, err := executeTemplateBlock(parsed, "text", data)
	if err != nil {
		return Message{}, err
	}
	subject = strings.Join(strings.Fields(subject), " ")
	text = strings.TrimSpace(text)
	if subject == "" || text == "" {
		return Message{}, Failure{Class: ErrorClassPermanent, Code: "template_empty"}
	}
	return Message{Subject: subject, Text: text}, nil
}

func executeTemplateBlock(parsed *template.Template, name string, data TemplateData) (string, error) {
	var output bytes.Buffer
	if err := parsed.ExecuteTemplate(&output, name, data); err != nil {
		return "", Failure{Class: ErrorClassPermanent, Code: "template_render_failed"}
	}
	return output.String(), nil
}

func validTemplateKey(value string) bool {
	switch value {
	case TemplateVPNAccess, TemplateVPNAccessReissued, TemplateSystemNotification:
		return true
	default:
		return false
	}
}

func validLocale(value string) bool {
	return value == "en" || value == "ru"
}
