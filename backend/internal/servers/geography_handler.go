package servers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type serverGeographyRepository interface {
	UpdateServerGeography(context.Context, string, UpdateServerGeographyInput) (Server, error)
}

func (h *Handler) UpdateGeography(w http.ResponseWriter, r *http.Request) {
	var request UpdateServerGeographyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must contain valid server geography JSON.")
		return
	}

	request.Country = strings.TrimSpace(request.Country)
	request.Region = strings.TrimSpace(request.Region)
	request.City = strings.TrimSpace(request.City)
	request.Source = strings.TrimSpace(request.Source)
	if request.Source == "" && (request.Latitude != nil || request.Longitude != nil || request.Country != "" || request.City != "") {
		request.Source = LocationSourceManual
	}
	if err := validateServerGeography(request); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	repository, ok := h.servers.(serverGeographyRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("geography_unavailable", "Server geography persistence is unavailable."))
		return
	}
	server, err := repository.UpdateServerGeography(r.Context(), r.PathValue("server_id"), UpdateServerGeographyInput{
		Country:   request.Country,
		Region:    request.Region,
		City:      request.City,
		Latitude:  request.Latitude,
		Longitude: request.Longitude,
		Source:    request.Source,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update server geography", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "server.geography.updated",
		ResourceType: "server",
		ResourceID:   server.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"country":         server.LocationCountry,
			"region":          server.LocationRegion,
			"city":            server.LocationCity,
			"location_source": server.LocationSource,
			"has_coordinates": server.LocationLatitude != nil && server.LocationLongitude != nil,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, server)
}

func validateServerGeography(request UpdateServerGeographyRequest) error {
	if len(request.Country) > 128 || len(request.Region) > 128 || len(request.City) > 128 {
		return errors.New("Country, region, and city must be 128 characters or fewer.")
	}
	if (request.Latitude == nil) != (request.Longitude == nil) {
		return errors.New("Latitude and longitude must be provided together.")
	}
	if request.Latitude != nil && (*request.Latitude < -90 || *request.Latitude > 90) {
		return errors.New("Latitude must be between -90 and 90.")
	}
	if request.Longitude != nil && (*request.Longitude < -180 || *request.Longitude > 180) {
		return errors.New("Longitude must be between -180 and 180.")
	}
	if request.Source != "" && request.Source != LocationSourceManual && request.Source != LocationSourceAutoDetected {
		return errors.New("Location source must be manual or auto_detected.")
	}
	if request.Latitude == nil && request.Longitude == nil && request.Source != "" && request.Country == "" && request.Region == "" && request.City == "" {
		return errors.New("Location source cannot be set without location data.")
	}
	return nil
}
