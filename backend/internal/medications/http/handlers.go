// Package http implementa los controladores HTTP del catálogo de medicamentos (PP-F3-API-003, ADR-016).
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/medications/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	searchapp "github.com/edcastanohunter/petpassport/backend/internal/search/app"
)

// MedicationHandler maneja los endpoints del catálogo de medicamentos.
type MedicationHandler struct {
	catalog *app.CatalogService
	search  *searchapp.ResilientSearchService
}

// NewMedicationHandler construye una nueva instancia de MedicationHandler.
func NewMedicationHandler(catalog *app.CatalogService, search *searchapp.ResilientSearchService) *MedicationHandler {
	return &MedicationHandler{
		catalog: catalog,
		search:  search,
	}
}

func principalFromReq(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token de autenticación"))
		return auth.Principal{}, false
	}
	return p, true
}

func isClinicalMember(p auth.Principal) bool {
	if p.HasRole("super_admin") {
		return true
	}
	for _, m := range p.Memberships {
		if m.OrgStatus == "active" && (m.Role == "veterinarian" || m.Role == "staff" || m.Role == "admin") {
			return true
		}
	}
	return false
}

// ListMedications maneja GET /catalogs/medications (PP-F3-API-003).
func (h *MedicationHandler) ListMedications(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !isClinicalMember(principal) {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Solo miembros clínicos pueden consultar el catálogo"))
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	var prescReq *bool
	if pr := q.Get("prescription_required"); pr != "" {
		b := pr == "true" || pr == "1"
		prescReq = &b
	}

	filter := app.MedicationFilter{
		Q:                    q.Get("q"),
		Species:              q.Get("species"),
		Form:                 q.Get("form"),
		PrescriptionRequired: prescReq,
		Status:               q.Get("status"),
		Cursor:               q.Get("cursor"),
		Limit:                limit,
	}

	meds, err := h.catalog.ListMedications(r.Context(), filter)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error consultando catálogo", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": meds})
}

// GetMedication maneja GET /catalogs/medications/{id} (PP-F3-API-003).
func (h *MedicationHandler) GetMedication(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !isClinicalMember(principal) {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Solo miembros clínicos pueden consultar el catálogo"))
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del id debe ser UUID"))
		return
	}

	med, mergedFrom, err := h.catalog.GetMedication(r.Context(), id)
	if err != nil {
		if errors.Is(err, infra.ErrNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "Medicamento no encontrado", "No existe ese medicamento en el catálogo"))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error obteniendo medicamento", err.Error()))
		return
	}

	resp := map[string]any{"data": med}
	if mergedFrom != nil {
		resp["merged_from"] = mergedFrom.String()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateSuggestion maneja POST /catalogs/medications/suggestions (PP-F3-API-003).
func (h *MedicationHandler) CreateSuggestion(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}

	var req struct {
		OrganizationID uuid.UUID `json:"organization_id"`
		Name           string    `json:"name"`
		Notes          *string   `json:"notes,omitempty"`
		PhotoObjectKey *string   `json:"photo_object_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Nombre requerido", "El nombre del medicamento es obligatorio"))
		return
	}

	// Validar que el usuario es veterinario en la organización
	isVet := false
	if principal.HasRole("super_admin") {
		isVet = true
	} else {
		for _, m := range principal.Memberships {
			if m.OrganizationID == req.OrganizationID && m.OrgStatus == "active" && m.Role == "veterinarian" {
				isVet = true
				break
			}
		}
	}
	if !isVet {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Solo veterinarios pueden sugerir medicamentos"))
		return
	}

	sug := domain.MedicationSuggestion{
		ProposedBy:     principal.UserID,
		OrganizationID: req.OrganizationID,
		Name:           req.Name,
		Notes:          req.Notes,
		PhotoObjectKey: req.PhotoObjectKey,
	}

	created, err := h.catalog.CreateSuggestion(r.Context(), sug)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error registrando sugerencia", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

// ListOrgSuggestions maneja GET /organizations/{org_id}/catalogs/medications/suggestions (PP-F3-API-003).
func (h *MedicationHandler) ListOrgSuggestions(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}

	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "Formato de org_id debe ser UUID"))
		return
	}

	// Validar que el usuario pertenece a la organización
	isMember := false
	if principal.HasRole("super_admin") {
		isMember = true
	} else {
		for _, m := range principal.Memberships {
			if m.OrganizationID == orgID && m.OrgStatus == "active" {
				isMember = true
				break
			}
		}
	}
	if !isMember {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "No eres miembro de esta organización"))
		return
	}

	status := r.URL.Query().Get("status")
	var stPtr *string
	if status != "" {
		stPtr = &status
	}

	list, err := h.catalog.ListSuggestions(r.Context(), &orgID, stPtr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error listando sugerencias", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": list})
}

// AdminCreateMedication maneja POST /admin/catalogs/medications (PP-F3-API-003).
func (h *MedicationHandler) AdminCreateMedication(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	var req struct {
		domain.Medication
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Nombre requerido", "El nombre es obligatorio"))
		return
	}

	med := req.Medication
	med.Source = domain.Source{Kind: "manual"}
	created, err := h.catalog.CreateMedication(r.Context(), principal.UserID, med, req.Reason)
	if err != nil {
		if errors.Is(err, infra.ErrDuplicateIdentity) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Medicamento duplicado", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error creando medicamento", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

// AdminUpdateMedication maneja PUT /admin/catalogs/medications/{id} (PP-F3-API-003).
func (h *MedicationHandler) AdminUpdateMedication(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	var req struct {
		domain.Medication
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	med := req.Medication
	med.ID = id
	updated, err := h.catalog.UpdateMedication(r.Context(), principal.UserID, med, req.Reason)
	if err != nil {
		if errors.Is(err, infra.ErrNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", err.Error()))
			return
		}
		if errors.Is(err, infra.ErrDuplicateIdentity) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Duplicado", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error actualizando medicamento", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// AdminDiscontinueMedication maneja POST /admin/catalogs/medications/{id}/discontinue (PP-F3-API-003).
func (h *MedicationHandler) AdminDiscontinueMedication(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	if err := h.catalog.DiscontinueMedication(r.Context(), principal.UserID, id, req.Reason); err != nil {
		if errors.Is(err, infra.ErrNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error descontinuando", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "discontinued", "id": id})
}

// AdminMergeMedication maneja POST /admin/catalogs/medications/{id}/merge (PP-F3-API-003).
func (h *MedicationHandler) AdminMergeMedication(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	var req struct {
		Into   uuid.UUID `json:"into"`
		Reason string    `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	if err := h.catalog.MergeMedication(r.Context(), principal.UserID, sourceID, req.Into, req.Reason); err != nil {
		if errors.Is(err, infra.ErrMergeChainTooDeep) || errors.Is(err, infra.ErrInvalidMergeTarget) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Fusión inválida", err.Error()))
			return
		}
		if errors.Is(err, infra.ErrNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error fusionando", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "merged", "merged_into": req.Into})
}

// AdminImport maneja POST /admin/catalogs/medications/import (PP-F3-API-003).
func (h *MedicationHandler) AdminImport(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	var req struct {
		Source string              `json:"source"`
		DryRun bool                `json:"dry_run"`
		Rows   []domain.Medication `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	batchID := uuid.Must(uuid.NewV7())
	now := time.Now()
	batch := domain.ImportBatch{
		ID:        batchID,
		Source:    req.Source,
		Status:    "completed",
		DryRun:    req.DryRun,
		CreatedBy: principal.UserID,
		CreatedAt: now,
	}

	var importRows []domain.ImportRow
	for _, m := range req.Rows {
		if m.Name == "" {
			batch.Errors = append(batch.Errors, "fila sin nombre")
			batch.Skipped++
			continue
		}
		if !req.DryRun {
			m.Source = domain.Source{
				Kind:       "import",
				Ref:        batchID.String(),
				ImportedAt: &now,
			}
			created, err := h.catalog.CreateMedication(r.Context(), principal.UserID, m, "importación lote "+batchID.String())
			if err != nil {
				batch.Errors = append(batch.Errors, fmt.Sprintf("%s: %v", m.Name, err))
				batch.Skipped++
			} else {
				batch.Created++
				importRows = append(importRows, domain.ImportRow{
					BatchID:      batchID,
					MedicationID: created.ID,
					Action:       "created",
				})
			}
		} else {
			batch.Created++
		}
	}

	if !req.DryRun {
		_ = h.catalog.SaveImportBatch(r.Context(), batch, importRows)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(batch)
}

// AdminGetImport maneja GET /admin/catalogs/medications/imports/{batch_id} (PP-F3-API-003).
func (h *MedicationHandler) AdminGetImport(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	batchID, err := uuid.Parse(chi.URLParam(r, "batch_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	batch, err := h.catalog.GetImportBatch(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, infra.ErrBatchNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error consultando lote", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(batch)
}

// AdminRevertImport maneja POST /admin/catalogs/medications/imports/{batch_id}/revert (PP-F3-API-003).
func (h *MedicationHandler) AdminRevertImport(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	batchID, err := uuid.Parse(chi.URLParam(r, "batch_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	if err := h.catalog.RevertImportBatch(r.Context(), principal.UserID, batchID); err != nil {
		if errors.Is(err, infra.ErrBatchNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", err.Error()))
			return
		}
		if errors.Is(err, infra.ErrBatchAlreadyReverted) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Ya revertido", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error revirtiendo lote", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "reverted", "batch_id": batchID})
}

// AdminListSuggestions maneja GET /admin/catalogs/medications/suggestions (PP-F3-API-003).
func (h *MedicationHandler) AdminListSuggestions(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	status := r.URL.Query().Get("status")
	var stPtr *string
	if status != "" {
		stPtr = &status
	}

	list, err := h.catalog.ListSuggestions(r.Context(), nil, stPtr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error listando sugerencias", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": list})
}

// AdminApproveSuggestion maneja POST /admin/catalogs/medications/suggestions/{id}/approve (PP-F3-API-003).
func (h *MedicationHandler) AdminApproveSuggestion(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	var med domain.Medication
	if err := json.NewDecoder(r.Body).Decode(&med); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	approved, err := h.catalog.ApproveSuggestion(r.Context(), principal.UserID, id, med)
	if err != nil {
		if errors.Is(err, infra.ErrSuggestionNotFound) {
			problem.Write(w, r, problem.New(problem.NotFound, "No encontrada", err.Error()))
			return
		}
		if errors.Is(err, infra.ErrSuggestionProcessed) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Ya procesada", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error aprobando", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(approved)
}

// AdminRejectSuggestion maneja POST /admin/catalogs/medications/suggestions/{id}/reject (PP-F3-API-003).
func (h *MedicationHandler) AdminRejectSuggestion(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "UUID inválido"))
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", "JSON mal formado"))
		return
	}

	if err := h.catalog.RejectSuggestion(r.Context(), principal.UserID, id, req.Reason); err != nil {
		if errors.Is(err, infra.ErrSuggestionProcessed) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Ya procesada", err.Error()))
			return
		}
		problem.Write(w, r, problem.New(problem.Internal, "Error rechazando", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "rejected"})
}

// AdminUsageStats maneja GET /admin/catalogs/medications/usage (PP-F3-API-003, PRD-005).
func (h *MedicationHandler) AdminUsageStats(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if d := authz.Can(principal, authz.AdminCatalogWrite, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	var fromPtr, toPtr *time.Time
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			fromPtr = &t
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			toPtr = &t
		}
	}

	stats, err := h.catalog.UsageStats(r.Context(), fromPtr, toPtr)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error calculando métricas de uso", err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// SuggestMedications maneja GET /suggest/medications (PP-F3-PRD-004, API-003).
func (h *MedicationHandler) SuggestMedications(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromReq(w, r)
	if !ok {
		return
	}
	if !isClinicalMember(principal) {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", "Solo miembros clínicos pueden consultar autocompletado"))
		return
	}

	q := r.URL.Query().Get("q")
	species := r.URL.Query().Get("species")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, mode, err := h.search.SuggestMedications(r.Context(), q, species, limit)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error en autocompletado", err.Error()))
		return
	}

	w.Header().Set("X-PP-Search-Mode", mode)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
}
