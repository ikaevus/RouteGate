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
	Status       string `json:"status"`
	VPNAccountID string `json:"vpnAccountId"`
	Message      string `json:"message"`
}
