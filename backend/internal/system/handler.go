package system

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/ikaevus/routegate/backend/internal/db"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type schemaVersionReader interface {
	AppliedSchemaVersion(context.Context) (string, error)
}

type Handler struct {
	logger *slog.Logger
	reader schemaVersionReader
	info   func() buildinfo.Info
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger: logger,
		reader: db.NewSchemaVersionRepository(pool),
		info:   buildinfo.Current,
	}
}

type VersionResponse struct {
	Manager            ManagerVersion            `json:"manager"`
	WebUI              WebUIVersion              `json:"webUi"`
	Database           DatabaseVersion           `json:"database"`
	AgentCompatibility AgentCompatibilityVersion `json:"agentCompatibility"`
	Update             UpdateVersion             `json:"update"`
}

type ManagerVersion struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
}

type WebUIVersion struct {
	Version string `json:"version"`
}

type DatabaseVersion struct {
	ExpectedSchemaVersion int     `json:"expectedSchemaVersion"`
	AppliedSchemaVersion  *string `json:"appliedSchemaVersion,omitempty"`
}

type AgentCompatibilityVersion struct {
	ProtocolVersion         int    `json:"protocolVersion"`
	MinimumProtocolVersion  int    `json:"minimumProtocolVersion"`
	RecommendedAgentVersion string `json:"recommendedAgentVersion"`
}

type UpdateVersion struct {
	Status                    string `json:"status"`
	Channel                   string `json:"channel"`
	AutomaticUpdatesSupported bool   `json:"automaticUpdatesSupported"`
}

func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	info := h.info()
	database := DatabaseVersion{
		ExpectedSchemaVersion: info.ExpectedDatabaseSchemaVersion,
	}

	applied, err := h.reader.AppliedSchemaVersion(r.Context())
	if err != nil {
		h.logger.Error("read applied schema version failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read system version."))
		return
	}
	if applied != "" {
		database.AppliedSchemaVersion = &applied
	}

	httpx.WriteJSON(w, http.StatusOK, VersionResponse{
		Manager: ManagerVersion{
			Version:   info.Version,
			GitCommit: info.GitCommit,
			BuildDate: info.BuildDate,
		},
		WebUI:    WebUIVersion{Version: info.WebUIVersion},
		Database: database,
		AgentCompatibility: AgentCompatibilityVersion{
			ProtocolVersion:         info.AgentProtocolVersion,
			MinimumProtocolVersion:  info.MinimumAgentProtocolVersion,
			RecommendedAgentVersion: info.RecommendedAgentVersion,
		},
		Update: UpdateVersion{
			Status:                    info.UpdateStatus,
			Channel:                   info.UpdateChannel,
			AutomaticUpdatesSupported: info.AutomaticUpdatesSupported,
		},
	})
}
