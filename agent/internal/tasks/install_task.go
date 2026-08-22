package tasks

import (
	"fmt"
	"strings"
)

const (
	InstallOperationWireGuard = "install_wireguard"
	InstallOperationHysteria2 = "install_hysteria2"
	InstallOperationMTG       = "install_mtg"
)

func ValidateVPNCoreInstallTask(task ConfigTask) error {
	if task.EffectiveKind() != TaskKindVPNCoreInstall {
		return fmt.Errorf("unsupported installation task kind %q", task.EffectiveKind())
	}
	switch strings.TrimSpace(task.Operation) {
	case InstallOperationSingBox, InstallOperationWireGuard, InstallOperationHysteria2, InstallOperationMTG:
		return nil
	default:
		return fmt.Errorf("unsupported installation operation %q", task.Operation)
	}
}
