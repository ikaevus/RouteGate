package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/artuazh/routegate/backend/internal/httpx"
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
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	if request.Email == "" || request.Password == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"email_and_password_required",
			"Email and password are required.",
		))
		return
	}

	h.logger.Info("dev login accepted", "email", request.Email)

	httpx.WriteJSON(w, http.StatusOK, LoginResponse{
		Token: devToken,
		User:  devUser(request.Email),
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "Bearer "+devToken {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error(
			"unauthorized",
			"Authentication is required.",
		))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, MeResponse{
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
