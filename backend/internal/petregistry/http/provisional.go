package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// ProvisionalPetRequest cuerpo para registrar una mascota provisional (PP-F3-API-007).
type ProvisionalPetRequest struct {
	Pet struct {
		Name         string                  `json:"name"`
		Species      string                  `json:"species"`
		SpeciesOther string                  `json:"species_other,omitempty"`
		Breed        *string                 `json:"breed,omitempty"`
		Sex          string                  `json:"sex"`
		Neutered     string                  `json:"neutered"`
		BirthDate    *domain.BirthDateInput  `json:"birth_date,omitempty"`
		Markings     *string                 `json:"markings,omitempty"`
		Microchip    *string                 `json:"microchip,omitempty"`
		Alerts       []domain.PetAlert       `json:"alerts,omitempty"`
	} `json:"pet"`
	GuardianContact struct {
		Email string `json:"email,omitempty"`
		Phone string `json:"phone,omitempty"`
	} `json:"guardian_contact"`
	SendInvitation bool `json:"send_invitation"`
}

// AcceptClaimRequest cuerpo para aceptar la reclamación de una mascota (PP-F3-API-007).
type AcceptClaimRequest struct {
	MergeIntoPetID *uuid.UUID `json:"merge_into_pet_id,omitempty"`
}

// RegisterProvisionalPet maneja POST /organizations/{org_id}/provisional-pets (PP-F3-API-007).
func (h *PetHandlers) RegisterProvisionalPet(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del org_id debe ser UUID"))
		return
	}

	if d := authz.Can(p, authz.PetRegisterProvisional, authz.OrgResource{ID: orgID}); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	var req ProvisionalPetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", err.Error()))
		return
	}

	cmd := domain.RegisterProvisionalPet{
		OrgID:         orgID,
		CreatedBy:     p.UserID,
		Name:          req.Pet.Name,
		Species:       req.Pet.Species,
		SpeciesOther:  req.Pet.SpeciesOther,
		Breed:         req.Pet.Breed,
		Sex:           req.Pet.Sex,
		Neutered:      req.Pet.Neutered,
		BirthDate:     req.Pet.BirthDate,
		Markings:      req.Pet.Markings,
		Microchip:     req.Pet.Microchip,
		Alerts:        req.Pet.Alerts,
		GuardianEmail: strings.TrimSpace(req.GuardianContact.Email),
		GuardianPhone: strings.TrimSpace(req.GuardianContact.Phone),
		Now:           h.now(),
	}

	ctx, _ := h.meta(r.Context(), p, r.Header.Get("Idempotency-Key"), 0)
	res, expiresAt, token, err := h.svc.RegisterProvisional(ctx, cmd, req.SendInvitation)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	type claimResp struct {
		Status    string     `json:"status"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		Token     string     `json:"token,omitempty"`
	}
	type respType struct {
		ID          uuid.UUID  `json:"id"`
		Provisional bool       `json:"provisional"`
		Claim       *claimResp `json:"claim,omitempty"`
	}

	out := respType{
		ID:          res.AggregateID,
		Provisional: true,
	}
	if req.SendInvitation && expiresAt != nil {
		out.Claim = &claimResp{
			Status:    "pending",
			ExpiresAt: expiresAt,
			Token:     token,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

// ResendPetClaim maneja POST /organizations/{org_id}/provisional-pets/{pet_id}/resend-claim (PP-F3-API-007).
func (h *PetHandlers) ResendPetClaim(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del org_id debe ser UUID"))
		return
	}
	petID, err := uuid.Parse(chi.URLParam(r, "pet_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del pet_id debe ser UUID"))
		return
	}

	if d := authz.Can(p, authz.PetRegisterProvisional, authz.OrgResource{ID: orgID}); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	ctx, _ := h.meta(r.Context(), p, r.Header.Get("Idempotency-Key"), 0)
	if err := h.svc.ResendClaim(ctx, orgID, petID); err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetPetClaimPreview maneja GET /pet-claims/{token} (PP-F3-API-007).
func (h *PetHandlers) GetPetClaimPreview(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	token := chi.URLParam(r, "token")
	if strings.TrimSpace(token) == "" {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Token requerido", "El token no puede estar vacío"))
		return
	}

	preview, err := h.svc.GetClaimPreview(r.Context(), token, p.UserID)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(preview)
}

// AcceptPetClaim maneja POST /pet-claims/{token}/accept (PP-F3-API-007).
func (h *PetHandlers) AcceptPetClaim(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}

	token := chi.URLParam(r, "token")
	if strings.TrimSpace(token) == "" {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Token requerido", "El token no puede estar vacío"))
		return
	}

	var req AcceptClaimRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, _ := h.meta(r.Context(), p, r.Header.Get("Idempotency-Key"), 0)
	res, err := h.svc.AcceptClaim(ctx, token, req.MergeIntoPetID)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	pet, err := h.queries.GetPet(r.Context(), res.AggregateID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": res.AggregateID})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pet)
}

// ListProvisionalPets maneja GET /organizations/{org_id}/provisional-pets (PP-F3-API-007).
func (h *PetHandlers) ListProvisionalPets(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del org_id debe ser UUID"))
		return
	}

	if d := authz.Can(p, authz.OrgPatientsList, authz.OrgResource{ID: orgID}); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	status := r.URL.Query().Get("status")
	pets, err := h.svc.ListProvisionalPets(r.Context(), orgID, status)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}
	if pets == nil {
		pets = []app.ProvisionalPetView{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": pets})
}

// ImportProvisionalPets maneja POST /organizations/{org_id}/provisional-pets/import (PP-F3-PRD-013).
func (h *PetHandlers) ImportProvisionalPets(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "org_id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del org_id debe ser UUID"))
		return
	}

	if d := authz.Can(p, authz.PetRegisterProvisional, authz.OrgResource{ID: orgID}); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	// Puede venir multipart/form-data o directamente como body CSV
	reader := r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err == nil {
			file, _, err := r.FormFile("file")
			if err == nil {
				defer file.Close()
				reader = file
			}
		}
	}

	ctx, _ := h.meta(r.Context(), p, r.Header.Get("Idempotency-Key"), 0)
	result, err := h.svc.ImportProvisionalPetsCSV(ctx, orgID, p.UserID, reader)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// MountProvisionalRoutes monta las rutas de pacientes provisionales en el router chi (PP-F3-API-007).
func (h *PetHandlers) MountProvisionalRoutes(r chi.Router) {
	r.Post("/organizations/{org_id}/provisional-pets", h.RegisterProvisionalPet)
	r.Post("/organizations/{org_id}/provisional-pets/import", h.ImportProvisionalPets)
	r.Get("/organizations/{org_id}/provisional-pets", h.ListProvisionalPets)
	r.Post("/organizations/{org_id}/provisional-pets/{pet_id}/resend-claim", h.ResendPetClaim)
	r.Get("/pet-claims/{token}", h.GetPetClaimPreview)
	r.Post("/pet-claims/{token}/accept", h.AcceptPetClaim)
}
