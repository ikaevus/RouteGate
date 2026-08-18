package platform

import "testing"

func TestParseDeploymentRole(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fallback  DeploymentRole
		want      DeploymentRole
		wantError bool
	}{
		{name: "management", value: " management ", fallback: DeploymentRoleVPN, want: DeploymentRoleManagement},
		{name: "vpn", value: "VPN", fallback: DeploymentRoleHybrid, want: DeploymentRoleVPN},
		{name: "hybrid", value: "hybrid", fallback: DeploymentRoleVPN, want: DeploymentRoleHybrid},
		{name: "default", fallback: DeploymentRoleVPN, want: DeploymentRoleVPN},
		{name: "invalid", value: "root", fallback: DeploymentRoleVPN, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseDeploymentRole(test.value, test.fallback)
			if (err != nil) != test.wantError {
				t.Fatalf("ParseDeploymentRole() error = %v, wantError %v", err, test.wantError)
			}
			if got != test.want {
				t.Fatalf("ParseDeploymentRole() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDeploymentRoleCapabilities(t *testing.T) {
	if !DeploymentRoleManagement.HostsManagementPlane() || DeploymentRoleManagement.HostsVPNPlane() {
		t.Fatal("management role capability mapping is invalid")
	}
	if DeploymentRoleVPN.HostsManagementPlane() || !DeploymentRoleVPN.HostsVPNPlane() {
		t.Fatal("VPN role capability mapping is invalid")
	}
	if !DeploymentRoleHybrid.HostsManagementPlane() || !DeploymentRoleHybrid.HostsVPNPlane() {
		t.Fatal("hybrid role must host both planes")
	}
}

func TestVPNCoreAdapterDescriptorSupportsDeclaredComposition(t *testing.T) {
	descriptor := VPNCoreAdapterDescriptor{
		Core:          VPNCoreSingBox,
		Protocol:      VPNProtocolVLESS,
		Transports:    []string{VPNTransportTCP},
		SecurityModes: []string{VPNSecurityNone, VPNSecurityReality},
	}

	for _, security := range []string{VPNSecurityNone, VPNSecurityReality} {
		if !descriptor.Supports(" SING-BOX ", "VLESS", "TCP", security) {
			t.Fatalf("expected descriptor to support VLESS/TCP/%s", security)
		}
	}
	if descriptor.Supports(VPNCoreSingBox, VPNProtocolVLESS, "quic", VPNSecurityReality) {
		t.Fatal("descriptor must reject an undeclared transport")
	}
	if descriptor.Supports(VPNCoreSingBox, "hysteria2", VPNTransportTCP, VPNSecurityReality) {
		t.Fatal("descriptor must reject another protocol")
	}
}
