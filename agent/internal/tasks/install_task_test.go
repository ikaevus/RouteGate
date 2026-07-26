package tasks

import "testing"

func TestValidateVPNCoreInstallTaskAllowsOnlySingBoxInstallation(t *testing.T) {
	if err := ValidateVPNCoreInstallTask(ConfigTask{Kind: TaskKindVPNCoreInstall, Operation: InstallOperationSingBox}); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	for _, task := range []ConfigTask{
		{Kind: TaskKindVPNCoreService, Operation: InstallOperationSingBox},
		{Kind: TaskKindVPNCoreInstall, Operation: "install_xray"},
		{Kind: TaskKindVPNCoreInstall, Operation: "apt install arbitrary"},
	} {
		if err := ValidateVPNCoreInstallTask(task); err == nil {
			t.Fatalf("unsafe task accepted: %+v", task)
		}
	}
}
