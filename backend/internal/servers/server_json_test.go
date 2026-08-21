package servers

import (
	"encoding/json"
	"testing"
)

func TestServerMarshalJSONSerializesINETAsHostAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publicIP  string
		privateIP string
		wantPub   string
		wantPriv  string
	}{
		{
			name:      "ipv4 host prefixes",
			publicIP:  "139.60.162.138/32",
			privateIP: "10.20.30.40/32",
			wantPub:   "139.60.162.138",
			wantPriv:  "10.20.30.40",
		},
		{
			name:      "ipv6 host prefix",
			publicIP:  "2001:db8::42/128",
			privateIP: "fd00::10/128",
			wantPub:   "2001:db8::42",
			wantPriv:  "fd00::10",
		},
		{
			name:      "already bare addresses",
			publicIP:  "203.0.113.25",
			privateIP: "192.0.2.12",
			wantPub:   "203.0.113.25",
			wantPriv:  "192.0.2.12",
		},
		{
			name:      "empty values",
			publicIP:  "",
			privateIP: "",
			wantPub:   "",
			wantPriv:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(Server{
				ID:        "server-id",
				Name:      "server",
				PublicIP:  tt.publicIP,
				PrivateIP: tt.privateIP,
				Status:    StatusActive,
			})
			if err != nil {
				t.Fatalf("marshal server: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}

			if got, _ := payload["publicIp"].(string); got != tt.wantPub {
				t.Fatalf("publicIp = %q, want %q", got, tt.wantPub)
			}
			if got, _ := payload["privateIp"].(string); got != tt.wantPriv {
				t.Fatalf("privateIp = %q, want %q", got, tt.wantPriv)
			}
		})
	}
}

func TestHostAddressPreservesUnexpectedValue(t *testing.T) {
	t.Parallel()

	const value = "not-an-ip"
	if got := hostAddress(value); got != value {
		t.Fatalf("hostAddress(%q) = %q, want original value", value, got)
	}
}
