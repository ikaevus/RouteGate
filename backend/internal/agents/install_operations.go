package agents

import "strings"

const (
	VPNCoreOperationInstallWireGuard = "install_wireguard"
	VPNCoreOperationInstallHysteria2 = "install_hysteria2"
	VPNCoreOperationInstallMTG       = "install_mtg"
)

func ValidVPNCoreInstallationOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case VPNCoreOperationInstallSingBox,
		VPNCoreOperationInstallWireGuard,
		VPNCoreOperationInstallHysteria2,
		VPNCoreOperationInstallMTG:
		return true
	default:
		return false
	}
}
