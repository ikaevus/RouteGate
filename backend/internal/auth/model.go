package auth

import "time"

type UserProfile struct {
	ID                string   `json:"id"`
	Email             string   `json:"email"`
	Username          string   `json:"username,omitempty"`
	DisplayName       string   `json:"display_name,omitempty"`
	DisplayNameLegacy string   `json:"displayName,omitempty"`
	UserType          string   `json:"user_type"`
	Status            string   `json:"status"`
	Roles             []string `json:"roles"`
	Permissions       []string `json:"permissions"`
}

type AuthenticatedUser struct {
	UserProfile
	SessionID string
}

type LoginRequest struct {
	Login    string `json:"login"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      UserProfile `json:"user"`
}

type MeResponse struct {
	User UserProfile `json:"user"`
}

type StatusResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
