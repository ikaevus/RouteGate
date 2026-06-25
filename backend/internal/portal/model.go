package portal

import "time"

const (
	AccessStatusActive    = "active"
	AccessStatusSuspended = "suspended"
	AccessStatusExpired   = "expired"
	AccessStatusPending   = "pending"
	AccessStatusNoAccess  = "no_access"
)

const (
	PortalSubscriptionFormat = "routegate.subscription.v1"
	PortalQRFormat           = "subscription-url"
)

type PortalUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Status      string `json:"status"`
}

type PortalProfile struct {
	ID           string     `json:"id"`
	DisplayName  string     `json:"displayName"`
	Status       string     `json:"status"`
	AccessStatus string     `json:"accessStatus"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	MaxDevices   *int       `json:"maxDevices,omitempty"`
	Protocol     string     `json:"protocol"`
	Location     string     `json:"location,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type PortalDashboard struct {
	AccessStatus      string               `json:"accessStatus"`
	ProfilesTotal     int                  `json:"profilesTotal"`
	ProfilesActive    int                  `json:"profilesActive"`
	NearestExpiration *time.Time           `json:"nearestExpiration,omitempty"`
	TrafficUsage      *TrafficUsageSummary `json:"trafficUsage,omitempty"`
	Notices           []PortalNotice       `json:"notices"`
}

type TrafficUsageSummary struct {
	Enabled bool `json:"enabled"`
}

type PortalNotice struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type PortalSubscription struct {
	ProfileID             string     `json:"profileId"`
	Available             bool       `json:"available"`
	AccessStatus          string     `json:"accessStatus"`
	SubscriptionURL       string     `json:"subscriptionUrl,omitempty"`
	Format                string     `json:"format"`
	ExpiresAt             *time.Time `json:"expiresAt,omitempty"`
	RequiresTokenRotation bool       `json:"requiresTokenRotation"`
	Message               string     `json:"message,omitempty"`
}

type PortalQRCode struct {
	ProfileID    string `json:"profileId"`
	Available    bool   `json:"available"`
	AccessStatus string `json:"accessStatus"`
	QRText       string `json:"qrText,omitempty"`
	Format       string `json:"format"`
	Message      string `json:"message,omitempty"`
}

type PortalSubscriptionToken struct {
	ID           string     `json:"-"`
	VPNAccountID string     `json:"-"`
	TokenHash    string     `json:"-"`
	Status       string     `json:"-"`
	ExpiresAt    *time.Time `json:"-"`
	CreatedAt    time.Time  `json:"-"`
	UpdatedAt    time.Time  `json:"-"`
}

type CreateSubscriptionTokenInput struct {
	VPNAccountID string
	TokenHash    string
	ExpiresAt    *time.Time
}

type InstructionPlatform struct {
	Platform    string `json:"platform"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type DeviceInstruction struct {
	Platform    string   `json:"platform"`
	DisplayName string   `json:"displayName"`
	Steps       []string `json:"steps"`
	Notes       []string `json:"notes"`
}
