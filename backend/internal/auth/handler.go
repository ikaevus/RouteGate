package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const devToken = "routegate-dev-token"

type Handler struct {
	logger *slog.Logger
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type User struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	Roles       []string `json:"roles"`
}

type MeResponse struct {
	User User `json:"user"`
}

type StatusResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{logger: logger}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, StatusResponse{
			Status:    "invalid_request",
			Timestamp: time.Now().UTC(),
		})
		return
	}

	if request.Email == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, StatusResponse{
			Status:    "email_and_password_required",
			Timestamp: time.Now().UTC(),
		})
		return
	}

	h.logger.Info("dev login accepted", "email", request.Email)

	writeJSON(w, http.StatusOK, LoginResponse{
		Token: devToken,
		User:  devUser(request.Email),
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "Bearer "+devToken {
		writeJSON(w, http.StatusUnauthorized, StatusResponse{
			Status:    "unauthorized",
			Timestamp: time.Now().UTC(),
		})
		return
	}

	writeJSON(w, http.StatusOK, MeResponse{
		User: devUser("admin@routegate.local"),
	})
}

func devUser(email string) User {
	return User{
		ID:          "dev-admin",
		Email:       email,
		DisplayName: "RouteGate Dev Admin",
		Roles:       []string{"admin"},
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
