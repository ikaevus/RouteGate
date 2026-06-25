package portal

import "github.com/ikaevus/routegate/backend/internal/auth"

type MeResponse struct {
	User PortalUser `json:"user"`
}

type DashboardResponse struct {
	Dashboard PortalDashboard `json:"dashboard"`
}

type ProfilesResponse struct {
	Items []PortalProfile `json:"items"`
}

type ProfileResponse struct {
	Profile PortalProfile `json:"profile"`
}

type SubscriptionResponse struct {
	Subscription PortalSubscription `json:"subscription"`
}

type QRCodeResponse struct {
	QR PortalQRCode `json:"qr"`
}

type InstructionsResponse struct {
	Items []InstructionPlatform `json:"items"`
}

type InstructionResponse struct {
	Instruction DeviceInstruction `json:"instruction"`
}

func portalUserFromAuthUser(user auth.AuthenticatedUser) PortalUser {
	return PortalUser{
		ID:          user.ID,
		Email:       user.Email,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Status:      user.Status,
	}
}
