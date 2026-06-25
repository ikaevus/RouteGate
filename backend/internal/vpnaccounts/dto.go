package vpnaccounts

import "time"

type CreateAccountRequest struct {
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	MaxDevices  *int       `json:"maxDevices"`
	ServerID    string     `json:"serverId"`
}

type UpdateAccountRequest struct {
	DisplayName *string    `json:"displayName"`
	Email       *string    `json:"email"`
	Status      *string    `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	MaxDevices  *int       `json:"maxDevices"`
	ServerID    *string    `json:"serverId"`
}

type CreateSubscriptionTokenRequest struct {
	ExpiresAt *time.Time `json:"expiresAt"`
}

type ListAccountsResponse struct {
	Items []Account `json:"items"`
}

type SubscriptionTokenResponse struct {
	VPNAccountID      string     `json:"vpnAccountId"`
	SubscriptionToken string     `json:"subscriptionToken"`
	SubscriptionURL   string     `json:"subscriptionUrl"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
}

type SubscriptionQRCodeResponse struct {
	VPNAccountID    string `json:"vpnAccountId"`
	SubscriptionURL string `json:"subscriptionUrl"`
	QRText          string `json:"qrText"`
	Format          string `json:"format"`
}

type PublicSubscriptionResponse struct {
	Status       string                    `json:"status"`
	Format       string                    `json:"format"`
	GeneratedAt  time.Time                 `json:"generatedAt"`
	VPNAccountID string                    `json:"vpnAccountId"`
	Account      PublicSubscriptionAccount `json:"account"`
	Server       *PublicSubscriptionServer `json:"server,omitempty"`
	Config       PublicSubscriptionConfig  `json:"config"`
}

type PublicSubscriptionAccount struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxDevices  *int       `json:"maxDevices,omitempty"`
}

type PublicSubscriptionServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname,omitempty"`
	PublicIP string `json:"publicIp,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Location string `json:"location,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type PublicSubscriptionConfig struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
