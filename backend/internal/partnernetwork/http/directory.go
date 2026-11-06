package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// DirectoryListingRequest es el cuerpo para actualizar la ficha de directorio.
type DirectoryListingRequest struct {
	Listed       bool     `json:"listed"`
	Services     []string `json:"services"`
	ShowPhone    bool     `json:"show_phone"`
	Neighborhood *string  `json:"neighborhood,omitempty"`
	GeoLat       *float64 `json:"geo_lat,omitempty"`
	GeoLon       *float64 `json:"geo_lon,omitempty"`
}

// UpdateDirectoryListing maneja PUT /organizations/{org_id}/directory-listing (PP-F3-API-005, PP-F3-DOM-010).
func (h *OrgHandlers) UpdateDirectoryListing(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del org_id debe ser UUID"))
		return
	}

	if err := can(p, authz.OrgUpdate, authz.OrgResource{ID: orgID}); err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	var req DirectoryListingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", err.Error()))
		return
	}

	cmd := domain.UpdateDirectoryListing{
		OrganizationID: orgID,
		Listed:         req.Listed,
		Services:       req.Services,
		ShowPhone:      req.ShowPhone,
		Neighborhood:   req.Neighborhood,
		GeoLat:         req.GeoLat,
		GeoLon:         req.GeoLon,
	}

	ctx := withCommand(r.Context(), p, "", 0)
	if _, err := h.svc.Dispatch(ctx, cmd); err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(req)
}

// SearchDirectory maneja GET /directory/organizations (PP-F3-API-005, SEC-008).
func (h *OrgHandlers) SearchDirectory(w http.ResponseWriter, r *http.Request) {
	if h.directory == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		return
	}

	q := r.URL.Query().Get("q")
	city := r.URL.Query().Get("city")
	country := r.URL.Query().Get("country")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
		limit = l
	}

	results, err := h.directory.Search(r.Context(), q, city, country, limit)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error buscando en el directorio", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": results})
}

// GetDirectoryOrganization maneja GET /directory/organizations/{id} (PP-F3-API-005).
func (h *OrgHandlers) GetDirectoryOrganization(w http.ResponseWriter, r *http.Request) {
	if h.directory == nil {
		problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", "Directorio no disponible"))
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del ID debe ser UUID"))
		return
	}

	org, err := h.directory.Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no existe o no es visible") {
			problem.Write(w, r, problem.New(problem.DirectoryNotListed, "Organización no listada", "La organización no está en el directorio público"))
			return
		}
		problem.Write(w, r, problem.From(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(org)
}

// MountDirectoryRoutes registra las rutas del directorio público y ajustes de organización.
func (h *OrgHandlers) MountDirectoryRoutes(r chi.Router) {
	r.Get("/directory/organizations", h.SearchDirectory)
	r.Get("/directory/organizations/{id}", h.GetDirectoryOrganization)
	r.Put("/organizations/{org_id}/directory-listing", h.UpdateDirectoryListing)
}
