package delivery

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

func buildMIMEMessage(message Message, fromAddress, fromName string, now time.Time) ([]byte, string, error) {
	recipient, err := normalizeEmailAddress(message.Recipient)
	if err != nil {
		return nil, "", Failure{Class: ErrorClassPermanent, Code: "invalid_recipient"}
	}
	from, err := normalizeEmailAddress(fromAddress)
	if err != nil {
		return nil, "", Failure{Class: ErrorClassPermanent, Code: "smtp_from_invalid"}
	}
	if hasHeaderBreak(message.Subject) || strings.TrimSpace(message.Subject) == "" {
		return nil, "", Failure{Class: ErrorClassPermanent, Code: "message_subject_invalid"}
	}
	if hasHeaderBreak(fromName) {
		return nil, "", Failure{Class: ErrorClassPermanent, Code: "smtp_from_invalid"}
	}
	if strings.TrimSpace(message.Text) == "" {
		return nil, "", Failure{Class: ErrorClassPermanent, Code: "message_body_missing"}
	}

	contentType, body, err := buildMIMEBody(message)
	if err != nil {
		return nil, "", err
	}

	var output bytes.Buffer
	fromHeader := (&mail.Address{Name: strings.TrimSpace(fromName), Address: from}).String()
	toHeader := (&mail.Address{Address: recipient}).String()
	writeHeaderLine(&output, "From", fromHeader)
	writeHeaderLine(&output, "To", toHeader)
	writeHeaderLine(&output, "Subject", mime.QEncoding.Encode("UTF-8", strings.TrimSpace(message.Subject)))
	writeHeaderLine(&output, "Date", now.UTC().Format(time.RFC1123Z))
	writeHeaderLine(&output, "MIME-Version", "1.0")
	writeHeaderLine(&output, "Content-Type", contentType)
	output.WriteString("\r\n")
	output.Write(body)
	return output.Bytes(), recipient, nil
}

func buildMIMEBody(message Message) (string, []byte, error) {
	alternativeType, alternativeBody, err := buildAlternativeBody(message.Text, message.HTML)
	if err != nil {
		return "", nil, err
	}
	if len(message.Attachments) == 0 {
		return alternativeType, alternativeBody, nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	firstHeader := make(textproto.MIMEHeader)
	firstHeader.Set("Content-Type", alternativeType)
	part, err := writer.CreatePart(firstHeader)
	if err != nil {
		return "", nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	if _, err := part.Write(alternativeBody); err != nil {
		return "", nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}

	for _, attachment := range message.Attachments {
		if err := writeMIMEAttachment(writer, attachment); err != nil {
			return "", nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return "", nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	contentType := mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": writer.Boundary()})
	return contentType, body.Bytes(), nil
}

func buildAlternativeBody(text, html string) (string, []byte, error) {
	if strings.TrimSpace(html) == "" {
		encoded, err := encodeQuotedPrintable(text)
		if err != nil {
			return "", nil, err
		}
		return "text/plain; charset=UTF-8", encoded, nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeQuotedPrintablePart(writer, "text/plain; charset=UTF-8", text); err != nil {
		return "", nil, err
	}
	if err := writeQuotedPrintablePart(writer, "text/html; charset=UTF-8", html); err != nil {
		return "", nil, err
	}
	if err := writer.Close(); err != nil {
		return "", nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	contentType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": writer.Boundary()})
	return contentType, body.Bytes(), nil
}

func writeQuotedPrintablePart(writer *multipart.Writer, contentType, value string) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", contentType)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(value)); err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	if err := qp.Close(); err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	return nil
}

func encodeQuotedPrintable(value string) ([]byte, error) {
	var output bytes.Buffer
	writer := quotedprintable.NewWriter(&output)
	if _, err := writer.Write([]byte(value)); err != nil {
		return nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	if err := writer.Close(); err != nil {
		return nil, Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	return output.Bytes(), nil
}

func writeMIMEAttachment(writer *multipart.Writer, attachment Attachment) error {
	filename := strings.TrimSpace(attachment.Filename)
	if filename == "" || len(filename) > 160 || hasHeaderBreak(filename) {
		return Failure{Class: ErrorClassPermanent, Code: "attachment_invalid"}
	}
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "attachment_invalid"}
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", contentType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	header.Set("Content-Transfer-Encoding", "base64")
	part, err := writer.CreatePart(header)
	if err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
	}
	encoded := base64.StdEncoding.EncodeToString(attachment.Content)
	for len(encoded) > 76 {
		if _, err := fmt.Fprintf(part, "%s\r\n", encoded[:76]); err != nil {
			return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
		}
		encoded = encoded[76:]
	}
	if encoded != "" {
		if _, err := fmt.Fprintf(part, "%s\r\n", encoded); err != nil {
			return Failure{Class: ErrorClassPermanent, Code: "message_encode_failed"}
		}
	}
	return nil
}

func normalizeEmailAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || hasHeaderBreak(value) {
		return "", fmt.Errorf("email address is empty or unsafe")
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address == "" {
		return "", fmt.Errorf("email address is invalid")
	}
	if strings.TrimSpace(parsed.Name) != "" || !strings.EqualFold(parsed.Address, value) {
		return "", fmt.Errorf("email address must not include a display name")
	}
	return parsed.Address, nil
}

func hasHeaderBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func writeHeaderLine(output *bytes.Buffer, key, value string) {
	output.WriteString(key)
	output.WriteString(": ")
	output.WriteString(value)
	output.WriteString("\r\n")
}
