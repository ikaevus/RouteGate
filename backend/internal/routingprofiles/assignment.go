package routingprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type routingProfileAssignmentRepository interface {
	GetServerAssignment(context.Context, string) (ServerRoutingProfileAssignment, error)
	AssignServerProfile(context.Context, AssignServerRoutingProfileInput) (ServerRoutingProfileAssignment, error)
	DeleteServerAssignment(context.Context, string) error
}

func (r *Repository) GetServerAssignment(ctx context.Context, serverID string) (ServerRoutingProfileAssignment, error) {
	exists, err := r.serverExists(ctx, serverID)
	if err != nil {
		return ServerRoutingProfileAssignment{}, err
	}
	if !exists {
		return ServerRoutingProfileAssignment{}, ErrServerNotFound
	}

	assignment, err := r.getServerAssignment(ctx, serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServerRoutingProfileAssignment{ServerID: serverID}, nil
	}
	return assignment, err
}

func (r *Repository) AssignServerProfile(ctx context.Context, input AssignServerRoutingProfileInput) (ServerRoutingProfileAssignment, error) {
	exists, err := r.serverExists(ctx, input.ServerID)
	if err != nil {
		return ServerRoutingProfileAssignment{}, err
	}
	if !exists {
		return ServerRoutingProfileAssignment{}, ErrServerNotFound
	}

	if _, err := r.GetProfile(ctx, input.RoutingProfileID); err != nil {
		return ServerRoutingProfileAssignment{}, err
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO server_routing_profiles (server_id, routing_profile_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (server_id) DO UPDATE
		SET routing_profile_id = EXCLUDED.routing_profile_id,
			updated_at = now()
	`, input.ServerID, input.RoutingProfileID)
	if err != nil {
		return ServerRoutingProfileAssignment{}, err
	}

	return r.getServerAssignment(ctx, input.ServerID)
}

func (r *Repository) DeleteServerAssignment(ctx context.Context, serverID string) error {
	exists, err := r.serverExists(ctx, serverID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrServerNotFound
	}

	_, err = r.pool.Exec(ctx, `DELETE FROM server_routing_profiles WHERE server_id = $1::uuid`, serverID)
	return err
}

func (r *Repository) getServerAssignment(ctx context.Context, serverID string) (ServerRoutingProfileAssignment, error) {
	var profile RoutingProfile
	var assignment ServerRoutingProfileAssignment
	var createdAt, updatedAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT
			srp.server_id::text,
			srp.created_at,
			srp.updated_at,
			p.id::text,
			p.name,
			COALESCE(p.description, ''),
			p.is_default,
			p.created_at,
			p.updated_at
		FROM server_routing_profiles srp
		JOIN routing_profiles p ON p.id = srp.routing_profile_id
		WHERE srp.server_id = $1::uuid
	`, serverID).Scan(
		&assignment.ServerID,
		&createdAt,
		&updatedAt,
		&profile.ID,
		&profile.Name,
		&profile.Description,
		&profile.IsDefault,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		return ServerRoutingProfileAssignment{}, err
	}
	assignment.CreatedAt = &createdAt
	assignment.UpdatedAt = &updatedAt
	assignment.RoutingProfile = &profile
	return assignment, nil
}

func (r *Repository) serverExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM servers WHERE id = $1::uuid)`, id).Scan(&exists)
	return exists, err
}

func (h *Handler) GetServerAssignment(w http.ResponseWriter, r *http.Request) {
	assignments, ok := h.profiles.(routingProfileAssignmentRepository)
	if !ok {
		h.databaseError(w, "get server routing profile assignment", ErrAssignmentRepositoryUnavailable)
		return
	}

	assignment, err := assignments.GetServerAssignment(r.Context(), r.PathValue("server_id"))
	if errors.Is(err, ErrServerNotFound) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get server routing profile assignment", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, assignment)
}

func (h *Handler) AssignServerProfile(w http.ResponseWriter, r *http.Request) {
	assignments, ok := h.profiles.(routingProfileAssignmentRepository)
	if !ok {
		h.databaseError(w, "assign server routing profile", ErrAssignmentRepositoryUnavailable)
		return
	}

	var request AssignServerRoutingProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	input := AssignServerRoutingProfileInput{
		ServerID:         r.PathValue("server_id"),
		RoutingProfileID: strings.TrimSpace(request.RoutingProfileID),
	}
	if err := validateAssignServerProfileInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	assignment, err := assignments.AssignServerProfile(r.Context(), input)
	if errors.Is(err, ErrServerNotFound) {
		writeServerNotFound(w)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "assign server routing profile", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, assignment)
}

func (h *Handler) DeleteServerAssignment(w http.ResponseWriter, r *http.Request) {
	assignments, ok := h.profiles.(routingProfileAssignmentRepository)
	if !ok {
		h.databaseError(w, "delete server routing profile assignment", ErrAssignmentRepositoryUnavailable)
		return
	}

	err := assignments.DeleteServerAssignment(r.Context(), r.PathValue("server_id"))
	if errors.Is(err, ErrServerNotFound) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "delete server routing profile assignment", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateAssignServerProfileInput(input AssignServerRoutingProfileInput) error {
	if input.ServerID == "" {
		return errors.New("server id is required")
	}
	if input.RoutingProfileID == "" {
		return errors.New("routing profile id is required")
	}
	return nil
}

func writeServerNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_not_found", "Server not found."))
}

var (
	ErrServerNotFound                  = errors.New("server not found")
	ErrAssignmentRepositoryUnavailable = errors.New("routing profile assignment repository unavailable")
)
