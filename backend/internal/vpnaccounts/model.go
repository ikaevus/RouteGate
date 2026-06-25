package vpnaccounts

import "time"

const (
	StatusCreated   = "created"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusExpired   = "expired"
	StatusRevoked   = "revoked"
)

const (
	SubscriptionTokenStatusActive  = "active"
	SubscriptionTokenStatusRevoked = "revoked"
)

const (
	RoutingActionDirect = "direct"
	RoutingActionVPN    = "vpn"
	RoutingActionBlock  = "block"
)

type Account struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxDevices  *int       `json:"maxDevices,omitempty"`
	ServerID    string     `json:"serverId,omitempty"`
	VLESSUUID   string     `json:"vlessUuid,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type SubscriptionToken struct {
	ID           string     `json:"id"`
	VPNAccountID string     `json:"vpnAccountId"`
	TokenHash    string     `json:"-"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type SubscriptionProfile struct {
	Account        Account
	Server         *SubscriptionServer
	Credentials    SubscriptionCredentials
	RoutingProfile *RoutingProfile
}

type SubscriptionCredentials struct {
	VLESS   VLESSCredentials
	Reality RealityCredentials
}

type VLESSCredentials struct {
	UUID    string
	Flow    string
	Network string
}

type RealityCredentials struct {
	PublicKey  string
	ShortID    string
	ServerName string
}

type SubscriptionServer struct {
	ID                string
	Name              string
	Hostname          string
	PublicIP          string
	Location          string
	Provider          string
	VLESSPort         int
	VLESSFlow         string
	VLESSNetwork      string
	RealityPublicKey  string
	RealityShortID    string
	RealityServerName string
}

type RoutingProfile struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	IsDefault   bool                 `json:"isDefault"`
	Rules       []RoutingProfileRule `json:"rules"`
}

type RoutingProfileRule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Priority       int      `json:"priority"`
	Action         string   `json:"action"`
	Domains        []string `json:"domains,omitempty"`
	DomainSuffixes []string `json:"domainSuffixes,omitempty"`
	DomainKeywords []string `json:"domainKeywords,omitempty"`
	IPCIDRs        []string `json:"ipCidrs,omitempty"`
	GeoSites       []string `json:"geoSites,omitempty"`
	GeoIPs         []string `json:"geoIps,omitempty"`
}

type CreateAccountInput struct {
	DisplayName string
	Email       string
	Status      string
	ExpiresAt   *time.Time
	MaxDevices  *int
	ServerID    string
}

type UpdateAccountInput struct {
	DisplayName *string
	Email       *string
	Status      *string
	ExpiresAt   *time.Time
	MaxDevices  *int
	ServerID    *string
}

type CreateSubscriptionTokenInput struct {
	VPNAccountID string
	TokenHash    string
	ExpiresAt    *time.Time
}

type AccountFilter struct {
	Status   string
	ServerID string
	Search   string
	Limit    int
	Offset   int
}

func ValidStatus(status string) bool {
	switch status {
	case StatusCreated, StatusActive, StatusSuspended, StatusExpired, StatusRevoked:
		return true
	default:
		return false
	}
}
