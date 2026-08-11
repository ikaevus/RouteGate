package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/config"
)

const (
	smtpTLSModeStartTLS = "starttls"
	smtpTLSModeTLS      = "tls"
)

type SMTPProvider struct {
	config       config.SMTPConfig
	fromAddress  string
	configError  string
	timeout      time.Duration
	now          func() time.Time
	dialContext  func(context.Context, string, string) (net.Conn, error)
	tlsDial      func(context.Context, string, string, *tls.Config) (net.Conn, error)
}

func NewSMTPProvider(cfg config.SMTPConfig) *SMTPProvider {
	provider := &SMTPProvider{
		config:  cfg,
		timeout: 15 * time.Second,
		now:     time.Now,
	}
	provider.dialContext = (&net.Dialer{}).DialContext
	provider.tlsDial = func(ctx context.Context, network, address string, tlsConfig *tls.Config) (net.Conn, error) {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{}, Config: tlsConfig}
		return dialer.DialContext(ctx, network, address)
	}
	provider.fromAddress, provider.configError = validateSMTPConfig(cfg)
	return provider
}

func (p *SMTPProvider) Name() string    { return "smtp" }
func (p *SMTPProvider) Channel() string { return "email" }

func (p *SMTPProvider) Configured() bool {
	return p != nil && p.configError == ""
}

func (p *SMTPProvider) ConfigurationErrorCode() string {
	if p == nil {
		return "smtp_not_configured"
	}
	return p.configError
}

func (p *SMTPProvider) Send(ctx context.Context, message Message) ProviderResult {
	if !p.Configured() {
		code := p.ConfigurationErrorCode()
		if code == "" {
			code = "smtp_not_configured"
		}
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: ErrorClassPermanent, ErrorCode: code}
	}

	rawMessage, recipient, err := buildMIMEMessage(message, p.fromAddress, p.config.FromName, p.now())
	if err != nil {
		failure := failureFromError(err, ErrorClassPermanent, "message_encode_failed")
		return ProviderResult{Outcome: OutcomePermanentFailure, ErrorClass: failure.Class, ErrorCode: failure.Code}
	}

	client, err := p.connect(ctx)
	if err != nil {
		return classifySMTPPreDataError(err, "smtp_connect_failed", ErrorClassTransient)
	}
	defer client.Close()

	if strings.TrimSpace(p.config.Username) != "" {
		auth := smtp.PlainAuth("", p.config.Username, p.config.Password, p.config.Host)
		if err := client.Auth(auth); err != nil {
			return classifySMTPPreDataError(err, "smtp_auth_failed", ErrorClassPermanent)
		}
	}
	if err := client.Mail(p.fromAddress); err != nil {
		return classifySMTPPreDataError(err, "smtp_mail_from_failed", ErrorClassPermanent)
	}
	if err := client.Rcpt(recipient); err != nil {
		return classifySMTPPreDataError(err, "invalid_recipient", ErrorClassPermanent)
	}
	writer, err := client.Data()
	if err != nil {
		return classifySMTPPreDataError(err, "smtp_data_rejected", ErrorClassTransient)
	}

	if _, err := writer.Write(rawMessage); err != nil {
		_ = writer.Close()
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "smtp_data_write_uncertain"}
	}
	if err := writer.Close(); err != nil {
		return ProviderResult{Outcome: OutcomeUncertain, ErrorClass: ErrorClassUncertain, ErrorCode: "smtp_acceptance_uncertain"}
	}

	_ = client.Quit()
	return ProviderResult{Outcome: OutcomeAccepted}
}

func (p *SMTPProvider) connect(ctx context.Context) (*smtp.Client, error) {
	host := strings.TrimSpace(p.config.Host)
	address := net.JoinHostPort(host, strconv.Itoa(p.config.Port))
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}

	var conn net.Conn
	var err error
	if p.config.TLSMode == smtpTLSModeTLS {
		conn, err = p.tlsDial(ctx, "tcp", address, tlsConfig)
	} else {
		conn, err = p.dialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}

	deadline := p.now().Add(p.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if p.config.TLSMode == smtpTLSModeStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, Failure{Class: ErrorClassPermanent, Code: "smtp_starttls_unavailable"}
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, Failure{Class: classifyTLSError(err), Code: "smtp_tls_failed"}
		}
	}
	return client, nil
}

func validateSMTPConfig(cfg config.SMTPConfig) (string, string) {
	host := strings.TrimSpace(cfg.Host)
	from := strings.TrimSpace(cfg.FromAddress)
	if host == "" && from == "" && strings.TrimSpace(cfg.Username) == "" && strings.TrimSpace(cfg.Password) == "" {
		return "", "smtp_not_configured"
	}
	if host == "" || from == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return "", "smtp_configuration_invalid"
	}
	if strings.ContainsAny(host, " \t\r\n/\\") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
		return "", "smtp_configuration_invalid"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if mode != smtpTLSModeStartTLS && mode != smtpTLSModeTLS {
		return "", "smtp_configuration_invalid"
	}
	if (strings.TrimSpace(cfg.Username) == "") != (strings.TrimSpace(cfg.Password) == "") {
		return "", "smtp_configuration_invalid"
	}
	if hasHeaderBreak(cfg.FromName) {
		return "", "smtp_configuration_invalid"
	}
	normalizedFrom, err := normalizeEmailAddress(from)
	if err != nil {
		return "", "smtp_configuration_invalid"
	}
	cfg.TLSMode = mode
	return normalizedFrom, ""
}

func classifySMTPPreDataError(err error, fallbackCode string, fallbackClass ErrorClass) ProviderResult {
	failure := failureFromError(err, fallbackClass, fallbackCode)
	if failure.Code != normalizeSafeCode(fallbackCode) {
		return resultForFailure(failure)
	}

	var smtpError *textproto.Error
	if errors.As(err, &smtpError) {
		if smtpError.Code >= 400 && smtpError.Code <= 499 {
			failure.Class = ErrorClassTransient
		} else if smtpError.Code >= 500 {
			failure.Class = ErrorClassPermanent
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		failure.Class = ErrorClassTransient
	}
	return resultForFailure(failure)
}

func resultForFailure(failure Failure) ProviderResult {
	failure.Class = normalizeErrorClass(failure.Class, ErrorClassPermanent)
	failure.Code = normalizeSafeCode(failure.Code)
	outcome := OutcomePermanentFailure
	if failure.Class == ErrorClassTransient {
		outcome = OutcomeRetryableFailure
	} else if failure.Class == ErrorClassUncertain {
		outcome = OutcomeUncertain
	}
	return ProviderResult{Outcome: outcome, ErrorClass: failure.Class, ErrorCode: failure.Code}
}

func classifyTLSError(err error) ErrorClass {
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return ErrorClassTransient
	}
	return ErrorClassPermanent
}
