package tasks

import (
	"fmt"
	"strings"
)

func ValidateVPNCoreInstallTask(task ConfigTask) error {
	if task.EffectiveKind() != TaskKindVPNCoreInstall {
		return fmt.Errorf("unsupported installation task kind %q", task.EffectiveKind())
	}
	if strings.TrimSpace(task.Operation) != InstallOperationSingBox {
		return fmt.Errorf("unsupported installation operation %q", task.Operation)
	}
	return nil
}
