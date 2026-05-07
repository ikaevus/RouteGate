package servers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	logger *slog.Logger
	mu     sync.RWMutex
	items  []Server
}

type Server struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname"`
	PublicIP  string    `json:"publicIp"`
	Location  string    `json:"location"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateServerRequest struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	PublicIP string `json:"publicIp"`
	Location string `json:"location"`
	Provider string `json:"provider"`
}

type ListServersResponse struct {
	Items []Server `json:"items"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func NewHandler(logger *slog.Logger) *Handler {
	now := time.Now().UTC()

	return &Handler{
		logger: logger,
		items: []Server{
			{
				ID:        "srv-dev-001",
				Name:      "Demo Finland VPS",
				Hostname:  "fi-demo.routegate.local",
				PublicIP:  "203.0.113.10",
				Location:  "Finland",
				Provider:  "Demo",
				Status:    "online",
				CreatedAt: now,
			},
		},
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	writeJSON(w, http.StatusOK, ListServersResponse{
		Items: h.items,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateServerRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "invalid_request",
			Message: "Request body must be valid JSON.",
		})
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	request.Hostname = strings.TrimSpace(request.Hostname)
	request.PublicIP = strings.TrimSpace(request.PublicIP)
	request.Location = strings.TrimSpace(request.Location)
	request.Provider = strings.TrimSpace(request.Provider)

	if request.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "name_required",
			Message: "Server name is required.",
		})
		return
	}

	now := time.Now().UTC()

	server := Server{
		ID:        "srv-dev-" + now.Format("20060102150405"),
		Name:      request.Name,
		Hostname:  request.Hostname,
		PublicIP:  request.PublicIP,
		Location:  request.Location,
		Provider:  request.Provider,
		Status:    "unknown",
		CreatedAt: now,
	}

	h.mu.Lock()
	h.items = append([]Server{server}, h.items...)
	h.mu.Unlock()

	h.logger.Info("dev server created", "id", server.ID, "name", server.Name)

	writeJSON(w, http.StatusCreated, server)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
