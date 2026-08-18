package diagnostics

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"strings"
	"time"
)

const certificateDialTimeout = 5 * time.Second

func collectManagerCertificate(managerURL string) map[string]any {
	target, err := url.Parse(strings.TrimSpace(managerURL))
	if err != nil || !strings.EqualFold(target.Scheme, "https") || strings.TrimSpace(target.Hostname()) == "" {
		return map[string]any{"available": false}
	}

	hostname := strings.TrimSpace(target.Hostname())
	port := target.Port()
	if port == "" {
		port = "443"
	}

	// The diagnostic obtains the peer chain without implicit verification and
	// then performs explicit x509 verification below. It sends no application
	// data and never returns certificate contents or private material.
	dialer := &net.Dialer{Timeout: certificateDialTimeout}
	connection, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(hostname, port), &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         hostname,
		InsecureSkipVerify: true, // Verification is explicit so expired/untrusted certificates remain observable.
	})
	if err != nil {
		return map[string]any{"available": false}
	}
	defer connection.Close()

	peerCertificates := connection.ConnectionState().PeerCertificates
	if len(peerCertificates) == 0 {
		return map[string]any{"available": false}
	}
	leaf := peerCertificates[0]
	intermediates := x509.NewCertPool()
	for _, certificate := range peerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	_, verifyErr := leaf.Verify(x509.VerifyOptions{
		DNSName:       hostname,
		Intermediates: intermediates,
	})

	return map[string]any{
		"available": true,
		"hostname":  hostname,
		"notBefore": leaf.NotBefore.UTC(),
		"notAfter":  leaf.NotAfter.UTC(),
		"verified":  verifyErr == nil,
	}
}
