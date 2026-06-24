package configs

type RenderConfigRequest struct {
	Comment string `json:"comment,omitempty"`
}

type RenderConfigResponse struct {
	ConfigVersion    ConfigVersion    `json:"configVersion"`
	ValidationResult ValidationResult `json:"validationResult"`
}

type ValidateConfigResponse struct {
	ConfigVersion    ConfigVersion    `json:"configVersion"`
	ValidationResult ValidationResult `json:"validationResult"`
}

type ApplyConfigRequest struct {
	Comment string `json:"comment,omitempty"`
}

type ApplyConfigResponse struct {
	Job ConfigApplyJob `json:"job"`
}

type ListConfigVersionsResponse struct {
	Items []ConfigVersion `json:"items"`
}

type ListConfigApplyJobsResponse struct {
	Items []ConfigApplyJob `json:"items"`
}
