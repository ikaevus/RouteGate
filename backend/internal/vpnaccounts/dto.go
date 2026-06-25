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

type ListAccountsResponse struct {
	Items []Account `json:"items"`
}
