package servers

import (
	"encoding/json"
	"net/netip"
)

// MarshalJSON keeps PostgreSQL INET as the storage type while exposing host
// addresses to API consumers. PostgreSQL renders INET values with a prefix
// (for example 139.60.162.138/32), but RouteGate server forms and public API
// treat publicIp/privateIp as host addresses rather than CIDR networks.
func (server Server) MarshalJSON() ([]byte, error) {
	type serverAlias Server

	payload := serverAlias(server)
	payload.PublicIP = hostAddress(server.PublicIP)
	payload.PrivateIP = hostAddress(server.PrivateIP)

	return json.Marshal(payload)
}

func hostAddress(value string) string {
	if value == "" {
		return ""
	}

	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String()
	}

	return value
}
