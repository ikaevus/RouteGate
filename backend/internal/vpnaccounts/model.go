package vpnaccounts

import "time"

const (
	StatusCreated   = "created"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusExpired   = "expired"
	StatusRevoked   = "revoked"
)

type Account struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxDevices  *int       `json:"maxDevices,omitempty"`
	ServerID    string     `json:"serverId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
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
