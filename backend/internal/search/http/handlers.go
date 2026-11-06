package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// SearchHandler maneja las peticiones HTTP del módulo de búsqueda (PP-F3-API-001, 002, 009).
type SearchHandler struct {
	search  *app.ResilientSearchService
	admin   *app.AdminService
	abuse   *app.AbuseDetector
	dcache  *cache.Cache
	cohorts *app.CohortService
	stats   *app.PlatformStatsService
}

// NewSearchHandler crea una nueva instancia del SearchHandler.
func NewSearchHandler(search *app.ResilientSearchService, admin *app.AdminService, abuse *app.AbuseDetector, dcache *cache.Cache) *SearchHandler {
	return &SearchHandler{
		search: search,
		admin:  admin,
		abuse:  abuse,
		dcache: dcache,
	}
}

func (h *SearchHandler) WithCohorts(c *app.CohortService) *SearchHandler {
	h.cohorts = c
	return h
}

func (h *SearchHandler) WithStats(s *app.PlatformStatsService) *SearchHandler {
	h.stats = s
	return h
}

func principalFromReq(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token de autenticación"))
		return auth.Principal{}, false
	}
	return p, true
}

// SearchPatients maneja GET /organizations/{org_id}/search/patients
func (h *SearchHandler) SearchPatients(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgIDStr := chi.URLParam(r, "org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID de organización inválido", "El formato del org_id debe ser UUID"))
		return
	}

	d := authz.Can(principal, authz.SearchPatients, authz.OrgResource{ID: orgID})
	if !d.Allow {
		if d.Reason == authz.ReasonOrgSuspended {
			problem.Write(w, r, problem.New(problem.OrganizationSuspended, "Organización suspendida", "La organización no tiene acceso"))
			return
		}
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "No tienes permiso para buscar pacientes en esta organización"))
		return
	}

	size := 20
	if s := r.URL.Query().Get("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 && parsed <= 50 {
			size = parsed
		}
	}

	req := app.PatientSearchRequest{
		OrgID:   orgID,
		Q:       r.URL.Query().Get("q"),
		Species: r.URL.Query().Get("species"),
		Status:  r.URL.Query().Get("status"),
		Cursor:  r.URL.Query().Get("cursor"),
		Size:    size,
	}

	res, err := h.search.SearchPatients(r.Context(), req, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Búsqueda no disponible", err.Error()))
		return
	}

	// Registrar para detección de scraping
	if h.abuse != nil && len(res.Data) > 0 {
		var pIDs []string
		for _, item := range res.Data {
			pIDs = append(pIDs, item.Pet.ID)
		}
		h.abuse.TrackQuery(r.Context(), principal.UserID, pIDs)
	}

	w.Header().Set("X-PP-Search-Mode", res.Mode)
	w.Header().Set("X-PP-Freshness", strconv.FormatInt(res.Freshness.LagSeconds, 10))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// SearchClinical maneja GET /organizations/{org_id}/search/clinical
func (h *SearchHandler) SearchClinical(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgIDStr := chi.URLParam(r, "org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID de organización inválido", "El formato del org_id debe ser UUID"))
		return
	}

	d := authz.Can(principal, authz.SearchClinical, authz.OrgResource{ID: orgID})
	if !d.Allow {
		if d.Reason == authz.ReasonOrgSuspended {
			problem.Write(w, r, problem.New(problem.OrganizationSuspended, "Organización suspendida", "La organización no tiene acceso"))
			return
		}
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "No tienes permiso para buscar historiales en esta organización"))
		return
	}

	size := 20
	if s := r.URL.Query().Get("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 && parsed <= 50 {
			size = parsed
		}
	}

	var fromTime, toTime *time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			fromTime = &t
		}
	}
	if tStr := r.URL.Query().Get("to"); tStr != "" {
		if t, err := time.Parse(time.RFC3339, tStr); err == nil {
			toTime = &t
		}
	}

	var profID *uuid.UUID
	if p := r.URL.Query().Get("professional_id"); p != "" {
		if u, err := uuid.Parse(p); err == nil {
			profID = &u
		}
	}

	req := app.ClinicalSearchRequest{
		OrgID:             orgID,
		Q:                 r.URL.Query().Get("q"),
		Species:           r.URL.Query().Get("species"),
		Type:              r.URL.Query().Get("type"),
		ProfessionalID:    profID,
		ConfirmedOnly:     r.URL.Query().Get("confirmed_only") == "true",
		OrganizationScope: r.URL.Query().Get("organization_scope"),
		From:              fromTime,
		To:                toTime,
		Cursor:            r.URL.Query().Get("cursor"),
		Size:              size,
	}

	res, err := h.search.SearchClinical(r.Context(), req, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Búsqueda clínica no disponible", err.Error()))
		return
	}

	// Registrar para detección de scraping
	if h.abuse != nil && len(res.Data) > 0 {
		var pIDs []string
		for _, group := range res.Data {
			pIDs = append(pIDs, group.Pet.ID)
		}
		h.abuse.TrackQuery(r.Context(), principal.UserID, pIDs)
	}

	w.Header().Set("X-PP-Search-Mode", res.Mode)
	w.Header().Set("X-PP-Freshness", strconv.FormatInt(res.Freshness.LagSeconds, 10))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// SearchMe maneja GET /me/search para tutores en sus propias mascotas
func (h *SearchHandler) SearchMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if principal.UserID == uuid.Nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere autenticación"))
		return
	}

	size := 20
	if s := r.URL.Query().Get("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 && parsed <= 50 {
			size = parsed
		}
	}

	req := app.ClinicalSearchRequest{
		OrgID:  uuid.Nil, // Tutor
		Q:      r.URL.Query().Get("q"),
		Cursor: r.URL.Query().Get("cursor"),
		Size:   size,
	}

	res, err := h.search.SearchClinical(r.Context(), req, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Búsqueda no disponible", err.Error()))
		return
	}

	w.Header().Set("X-PP-Search-Mode", res.Mode)
	w.Header().Set("X-PP-Freshness", strconv.FormatInt(res.Freshness.LagSeconds, 10))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// SuggestPatients maneja GET /organizations/{org_id}/suggest/patients
func (h *SearchHandler) SuggestPatients(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgIDStr := chi.URLParam(r, "org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID de organización inválido", "El formato del org_id debe ser UUID"))
		return
	}

	d := authz.Can(principal, authz.SearchPatients, authz.OrgResource{ID: orgID})
	if !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "No tienes permiso para autocompletar pacientes en esta organización"))
		return
	}

	q := r.URL.Query().Get("q")
	items, mode, err := h.search.SuggestPatients(r.Context(), orgID, q, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Autocompletado no disponible", err.Error()))
		return
	}

	w.Header().Set("X-PP-Search-Mode", mode)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
}

// SuggestPetsMe maneja GET /me/suggest/pets para autocompletar mascotas del tutor
func (h *SearchHandler) SuggestPetsMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if principal.UserID == uuid.Nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere autenticación"))
		return
	}

	q := r.URL.Query().Get("q")
	items, mode, err := h.search.SuggestPatients(r.Context(), uuid.Nil, q, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Autocompletado no disponible", err.Error()))
		return
	}

	w.Header().Set("X-PP-Search-Mode", mode)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
}

// AdminSearchClinical maneja GET /admin/search/clinical (super_admin con motivo obligatorio)
func (h *SearchHandler) AdminSearchClinical(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	d := authz.Can(principal, authz.AdminSearch, nil)
	if !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Solo super_admin puede realizar búsquedas administrativas"))
		return
	}

	reason := r.URL.Query().Get("reason")
	if strings.TrimSpace(reason) == "" {
		problem.Write(w, r, problem.New(problem.AdminReasonRequired, "Motivo requerido", "Las búsquedas de super_admin exigen motivo por auditoría"))
		return
	}

	req := app.ClinicalSearchRequest{
		OrgID: uuid.Nil,
		Q:     r.URL.Query().Get("q"),
		Size:  20,
	}

	res, err := h.search.SearchClinical(r.Context(), req, principal)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchUnavailable, "Búsqueda no disponible", err.Error()))
		return
	}

	w.Header().Set("X-PP-Search-Mode", res.Mode)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// ListIndices maneja GET /admin/search/indices
func (h *SearchHandler) ListIndices(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !principal.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	summaries, err := h.admin.ListIndices(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error listando índices", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summaries)
}

// ReindexIndex maneja POST /admin/search/indices/{alias}/reindex
func (h *SearchHandler) ReindexIndex(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !principal.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	alias := chi.URLParam(r, "alias")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if strings.TrimSpace(body.Reason) == "" {
		problem.Write(w, r, problem.New(problem.AdminReasonRequired, "Motivo requerido", "El reindex exige motivo por auditoría"))
		return
	}

	jobID, err := h.admin.Reindex(r.Context(), alias, body.Reason, &principal.UserID)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error iniciando reindex", err.Error()))
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"job_id": jobID.String()})
}

// RetryQueue maneja POST /admin/search/index-queue/retry
func (h *SearchHandler) RetryQueue(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !principal.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	count, err := h.admin.RetryIndexQueue(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error reintentando cola", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"retried": count})
}

// PurgeCache maneja POST /admin/cache/purge
func (h *SearchHandler) PurgeCache(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !principal.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	var body struct {
		Reason   string   `json:"reason"`
		Families []string `json:"families"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if strings.TrimSpace(body.Reason) == "" {
		problem.Write(w, r, problem.New(problem.AdminReasonRequired, "Motivo requerido", "La purga de caché exige motivo por auditoría"))
		return
	}

	// Purga selectiva
	w.WriteHeader(http.StatusAccepted)
}

// GetCohorts maneja GET /organizations/{org_id}/cohorts (PP-F3-API-004)
func (h *SearchHandler) GetCohorts(w http.ResponseWriter, r *http.Request) {
	if _, ok := principalFromReq(w, r); !ok {
		return
	}
	if h.cohorts == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": h.cohorts.Definitions()})
}

// RunCohort maneja POST /organizations/{org_id}/cohorts/{key}/run (PP-F3-API-004)
func (h *SearchHandler) RunCohort(w http.ResponseWriter, r *http.Request) {
	_, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "OrgID inválido", err.Error()))
		return
	}
	key := chi.URLParam(r, "key")

	var params map[string]any
	_ = json.NewDecoder(r.Body).Decode(&params)
	if params == nil {
		params = make(map[string]any)
	}

	if h.cohorts == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "total": 0, "truncated": false})
		return
	}

	rows, total, truncated, err := h.cohorts.Run(r.Context(), orgID, key, params)
	if err != nil {
		problem.Write(w, r, problem.New(problem.SearchQueryInvalid, "Error ejecutando cohorte", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":      rows,
		"total":     total,
		"truncated": truncated,
	})
}

// ExportCohort maneja POST /organizations/{org_id}/cohorts/{key}/export (PP-F3-API-004, RN-20)
func (h *SearchHandler) ExportCohort(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "OrgID inválido", err.Error()))
		return
	}
	key := chi.URLParam(r, "key")

	var body struct {
		Reason string         `json:"reason"`
		Params map[string]any `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if strings.TrimSpace(body.Reason) == "" {
		problem.Write(w, r, problem.New(problem.ReasonRequired, "Motivo requerido", "La exportación de cohorte exige motivo de auditoría"))
		return
	}

	if h.cohorts == nil {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		return
	}

	csvData, err := h.cohorts.Export(r.Context(), orgID, p.UserID, key, body.Reason, body.Params)
	if err != nil {
		if err == domain.ErrCohortTooLarge {
			problem.Write(w, r, problem.New(problem.CohortTooLarge, "Cohorte demasiado amplia", "La exportación supera el límite de 1.000 filas. Refine los parámetros."))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error exportando cohorte", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cohort-%s-%s.csv"`, key, time.Now().Format("20060102")))
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- CSV data returned with text/csv content type and attachment disposition
	_, _ = w.Write(csvData)
}

// NotifyCohort maneja POST /organizations/{org_id}/cohorts/{key}/notify (PP-F3-API-004)
func (h *SearchHandler) NotifyCohort(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "OrgID inválido", err.Error()))
		return
	}
	key := chi.URLParam(r, "key")

	var body struct {
		Message string         `json:"message"`
		Params  map[string]any `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if strings.TrimSpace(body.Message) == "" || len(body.Message) > 500 {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Mensaje inválido", "El mensaje debe tener entre 1 y 500 caracteres"))
		return
	}

	if h.cohorts == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"scheduled": 0, "skipped_by_preference": 0})
		return
	}

	res, err := h.cohorts.Notify(r.Context(), orgID, p.UserID, key, body.Params, body.Message)
	if err != nil {
		if err == domain.ErrCampaignRateLimited {
			problem.Write(w, r, problem.New(problem.CampaignRateLimited, "Campaña limitada por frecuencia", "Solo se permite una campaña por cohorte cada 7 días"))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error notificando cohorte", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(res)
}

// GetPlatformStats maneja GET /admin/platform/stats (PP-F3-API-006)
func (h *SearchHandler) GetPlatformStats(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !p.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	granularity := r.URL.Query().Get("granularity")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	if h.stats == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"series": []any{}})
		return
	}

	series, err := h.stats.GetStats(r.Context(), granularity, from, to)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error consultando estadísticas", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"series": series})
}

// GetTopOrganizations maneja GET /admin/platform/top-organizations (PP-F3-API-006)
func (h *SearchHandler) GetTopOrganizations(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !p.HasRole("super_admin") {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Se requiere super_admin"))
		return
	}

	metric := r.URL.Query().Get("metric")
	period := r.URL.Query().Get("period")

	if h.stats == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		return
	}

	data, err := h.stats.GetTopOrganizations(r.Context(), metric, period)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error consultando top organizaciones", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
