// Package http implementa las operaciones de Clinical Records del contrato
// OpenAPI (PP-F2-API-001/002/003/013) sobre el strict server generado. Cada
// handler: principal → vista de autorización (Access + PetAccess) →
// authz.Can con la x-pp-action del contrato (404 si el encuentro no es
// visible, 403 si es visible sin permiso) → comando por el bus (If-Match o
// Idempotency-Key) → respuesta con ETag. Las lecturas clínicas de miembros
// de organización se auditan (SEC-005).
package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// presignTTL es la vigencia de las URLs prefirmadas (ARQ-011).
const presignTTL = 15 * time.Minute

// Commands es la fachada de escritura y verificación de app.Service.
type Commands interface {
	Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error)
	VerifyEncounter(ctx context.Context, id uuid.UUID, hash string) (app.VerifyView, error)
}

// Deps son las dependencias de los handlers.
type Deps struct {
	Commands  Commands
	Queries   app.EncounterQueries
	Pets      app.PetAccess
	Documents app.Documents
	Uploads   app.StagedUploads
	Freshness app.Freshness
	Auditor   app.AccessAuditor
	Idem      *idempotency.Store
	Objects   storage.ObjectStorage // nil = adjuntos y PDF deshabilitados
	Logger    *slog.Logger

	CardDeps
}

// ClinicalHandlers implementa las operaciones de Clinical del contrato.
type ClinicalHandlers struct {
	Deps
	now func() time.Time
}

// NewClinicalHandlers construye los handlers.
func NewClinicalHandlers(d Deps) *ClinicalHandlers {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &ClinicalHandlers{Deps: d, now: time.Now}
}

func (h *ClinicalHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "sin principal en el contexto"}
	}
	return p, nil
}

// meta construye el CommandContext del request (DOM-012).
func (h *ClinicalHandlers) meta(ctx context.Context, p auth.Principal, idemKey string, expected int64) context.Context {
	info := reqctx.From(ctx)
	return app.WithCommandContext(ctx, app.CommandContext{
		Principal: app.PrincipalInfo{
			UserID: p.UserID, Email: p.Email, Roles: p.GlobalRoles, SuperAdmin: p.HasRole("super_admin"),
		},
		CorrelationID: info.CorrelationID, CausationID: info.RequestID, Source: "api",
		IdempotencyKey: idemKey, ExpectedVersion: expected,
	})
}

// ------------------------------------------------------- autorización

// encounter carga la vista de autorización del encuentro (404 si no existe).
func (h *ClinicalHandlers) encounter(ctx context.Context, id uuid.UUID) (app.EncounterAccess, authz.EncounterResource, error) {
	acc, err := h.Queries.Access(ctx, id)
	if err != nil {
		return app.EncounterAccess{}, authz.EncounterResource{}, err
	}
	linked, err := h.Pets.LinkedOrgIDs(ctx, acc.PetID)
	if err != nil {
		return app.EncounterAccess{}, authz.EncounterResource{}, err
	}
	return acc, authz.EncounterResource{
		ID: acc.ID, PetID: acc.PetID, OrganizationID: acc.OrganizationID, GuardianID: acc.GuardianID,
		AttendingVetID: acc.AttendingVetID, Status: acc.Status,
		PetLinked: slices.Contains(linked, acc.OrganizationID), LinkedOrgIDs: linked,
	}, nil
}

// authorize evalúa la acción: no visible → 404 (no revelar existencia,
// ARQ-010); visible sin permiso → 403. authz.Can se llama siempre.
func authorize(p auth.Principal, action authz.Action, res authz.EncounterResource) error {
	d := authz.Can(p, action, res)
	if d.Allow {
		return nil
	}
	if !authz.EncounterVisible(p, res) {
		return &kernel.NotFoundError{Resource: "encounter"}
	}
	return &kernel.ForbiddenError{Reason: d.Reason}
}

// load = encounter + authorize.
func (h *ClinicalHandlers) load(ctx context.Context, p auth.Principal, id uuid.UUID, action authz.Action) (app.EncounterAccess, authz.EncounterResource, error) {
	acc, res, err := h.encounter(ctx, id)
	if err != nil {
		return acc, res, err
	}
	return acc, res, authorize(p, action, res)
}

// actingOrg devuelve la primera organización candidata donde el principal es
// miembro clínico activo (para la auditoría SEC-005).
func actingOrg(p auth.Principal, candidates ...uuid.UUID) *uuid.UUID {
	for _, c := range candidates {
		for _, m := range p.Memberships {
			if m.OrganizationID == c && m.OrgStatus == "active" && m.OrgType == "clinic" {
				id := c
				return &id
			}
		}
	}
	return nil
}

// audit registra la lectura clínica de un no-tutor (SEC-005). Un fallo de
// auditoría no expone datos: se devuelve como error interno.
func (h *ClinicalHandlers) audit(ctx context.Context, p auth.Principal, guardianID uuid.UUID, org *uuid.UUID, petID uuid.UUID, resource string, resourceID uuid.UUID) error {
	if p.UserID == guardianID || h.Auditor == nil {
		return nil
	}
	reason := ""
	if org == nil && p.HasRole("super_admin") {
		reason = "super_admin"
	}
	return h.Auditor.RecordRead(ctx, app.ReadAudit{
		ActorID: p.UserID, ActorRoles: p.GlobalRoles, OrganizationID: org, PetID: petID,
		Resource: resource, ResourceID: resourceID, Reason: reason,
		CorrelationID: reqctx.From(ctx).CorrelationID,
	})
}

// --------------------------------------------------------- respuestas

// view compone el DTO del encuentro para el principal.
func (h *ClinicalHandlers) view(ctx context.Context, p auth.Principal, id uuid.UUID) (gen.Encounter, int64, error) {
	v, err := h.Queries.Get(ctx, id)
	if err != nil {
		return gen.Encounter{}, 0, err
	}
	guardian, err := h.isGuardianView(ctx, p, v.PetID)
	if err != nil {
		return gen.Encounter{}, 0, err
	}
	o := viewOpts{guardian: guardian}
	if !guardian && v.Status == string(domain.StatusSigned) && h.Uploads != nil {
		if o.pending, err = h.Uploads.Pending(ctx, id); err != nil {
			return gen.Encounter{}, 0, err
		}
	}
	if v.PDF != nil && v.PDF.Status == "ready" && v.PDF.ObjectKey != "" && h.Objects != nil {
		if u, err := h.presignPDF(ctx, v.PDF.ObjectKey, v.ID); err == nil {
			exp := h.now().Add(presignTTL)
			o.pdfURL, o.pdfExp = u, &exp
		}
	}
	return toEncounter(v, o), v.Version, nil
}

// isGuardianView: el principal es el tutor (y no actúa como miembro de org).
func (h *ClinicalHandlers) isGuardianView(ctx context.Context, p auth.Principal, petID uuid.UUID) (bool, error) {
	g, err := h.Pets.GuardianOf(ctx, petID)
	if err != nil {
		return false, err
	}
	return g == p.UserID, nil
}

// wantsJSON: con `Accept: application/json` las descargas responden 200
// DownloadLink en vez de 302 (la SPA no puede leer Location con fetch).
func wantsJSON(accept *gen.AcceptLink) bool {
	return accept != nil && strings.Contains(strings.ToLower(*accept), "application/json")
}

func (h *ClinicalHandlers) presignPDF(ctx context.Context, key string, encounterID uuid.UUID) (string, error) {
	if dp, ok := h.Objects.(storage.DownloadPresigner); ok {
		return dp.PresignDownload(ctx, key, presignTTL, "consulta-"+encounterID.String()+".pdf", "application/pdf")
	}
	return h.Objects.PresignGet(ctx, key, presignTTL)
}

// mutate ejecuta un comando de escritura sobre un encuentro existente y
// devuelve el DTO actualizado.
func (h *ClinicalHandlers) mutate(ctx context.Context, id uuid.UUID, action authz.Action, ifMatch int64, build func(acc app.EncounterAccess) (bus.Command, error)) (gen.Encounter, int64, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return gen.Encounter{}, 0, err
	}
	acc, _, err := h.load(ctx, p, id, action)
	if err != nil {
		return gen.Encounter{}, 0, err
	}
	cmd, err := build(acc)
	if err != nil {
		return gen.Encounter{}, 0, err
	}
	if _, err := h.Commands.Dispatch(h.meta(ctx, p, "", ifMatch), cmd); err != nil {
		return gen.Encounter{}, 0, mapErr(err)
	}
	return h.view(ctx, p, id)
}

// mapErr traduce IncompleteError (E6/E7) a 422 ENCOUNTER_INCOMPLETE con
// errors[] (FE-004: checklist de firma).
func mapErr(err error) error {
	var inc *domain.IncompleteError
	if errors.As(err, &inc) {
		pr := problem.New(problem.EncounterIncomplete, "Consulta incompleta", "Falta contenido mínimo para firmar.")
		for _, m := range inc.Missing {
			pr.Errors = append(pr.Errors, problem.FieldError{Field: m, Code: "REQUIRED"})
		}
		return &problem.DirectError{P: pr}
	}
	return err
}

// -------------------------------------------------- organización: lista

// ListOrgEncounters lista las consultas de la organización (API-001).
func (h *ClinicalHandlers) ListOrgEncounters(ctx context.Context, req gen.ListOrgEncountersRequestObject) (gen.ListOrgEncountersResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if d := authz.Can(p, authz.EncounterList, authz.EncounterResource{ID: orgID, OrganizationID: orgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	cursor, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	f := app.EncounterFilter{OrganizationID: orgID, Cursor: cursor, Limit: limitOf(req.Params.Limit),
		From: req.Params.From, To: req.Params.To}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if req.Params.PetId != nil {
		f.PetID = ptr(uuid.UUID(*req.Params.PetId))
	}
	if req.Params.ProfessionalId != nil {
		f.ProfessionalID = ptr(uuid.UUID(*req.Params.ProfessionalId))
	}
	items, next, err := h.Queries.ListByOrg(ctx, f)
	if err != nil {
		return nil, err
	}
	// Borradores de mascotas sin vínculo vigente: la org solo conserva lo
	// firmado (custodia, ADR-010).
	linkedByPet := map[uuid.UUID]bool{}
	page := gen.EncounterPage{Data: make([]gen.EncounterSummary, 0, len(items))}
	for _, it := range items {
		if it.Status != string(domain.StatusSigned) {
			linked, seen := linkedByPet[it.PetID]
			if !seen {
				ids, err := h.Pets.LinkedOrgIDs(ctx, it.PetID)
				if err != nil {
					return nil, err
				}
				linked = slices.Contains(ids, orgID)
				linkedByPet[it.PetID] = linked
			}
			if !linked {
				continue
			}
		}
		page.Data = append(page.Data, toSummary(it))
	}
	if next != nil {
		page.NextCursor = ptr(encodeCursor(*next))
	}
	return gen.ListOrgEncounters200JSONResponse(page), nil
}

// ------------------------------------------------------------ abrir

// OpenEncounter abre una consulta en borrador (Idempotency-Key, E1/E2).
func (h *ClinicalHandlers) OpenEncounter(ctx context.Context, req gen.OpenEncounterRequestObject) (gen.OpenEncounterResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	b := req.Body
	if b == nil {
		b = &gen.OpenEncounterRequest{}
	}
	petID := uuid.UUID(b.PetId)
	res := authz.EncounterResource{ID: uuid.New(), PetID: petID, OrganizationID: orgID}
	if petID != uuid.Nil {
		level, err := h.Pets.CanOrgAccess(ctx, petID, orgID)
		if err != nil {
			return nil, err
		}
		res.PetLinked = level == app.AccessLinked
	}
	if d := authz.Can(p, authz.EncounterOpen, res); !d.Allow {
		if actingOrg(p, orgID) != nil {
			return nil, domain.ErrPetNotAccessible // miembro sin vínculo (E1, 403)
		}
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	hash := sha256.Sum256(httpserver.RawBodyFrom(ctx))
	outcome, err := h.Idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayed{status: outcome.Status, body: outcome.Body}, nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	cmd := domain.OpenEncounter{
		EncounterID: id, PetID: petID, OrganizationID: orgID,
		Type: string(b.Type), Reason: b.Reason,
	}
	if b.StartedAt != nil {
		cmd.StartedAt = *b.StartedAt
	}
	if b.AttendingVetId != nil {
		cmd.AttendingVetID = ptr(uuid.UUID(*b.AttendingVetId))
	}
	if b.AppointmentId != nil {
		cmd.AppointmentID = ptr(uuid.UUID(*b.AppointmentId))
	}
	if _, err := h.Commands.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, mapErr(err)
	}
	body, version, err := h.view(ctx, p, id)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	location := "/api/v1/encounters/" + id.String()
	return gen.OpenEncounter201JSONResponse{
		Body:    body,
		Headers: gen.OpenEncounter201ResponseHeaders{ETag: &version, Location: &location},
	}, nil
}

// ------------------------------------------------------------ leer

// GetEncounter devuelve la consulta (tutor: solo firmadas, adjuntos visibles).
func (h *ClinicalHandlers) GetEncounter(ctx context.Context, req gen.GetEncounterRequestObject) (gen.GetEncounterResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	acc, res, err := h.load(ctx, p, uuid.UUID(req.EncounterId), authz.EncounterRead)
	if err != nil {
		return nil, err
	}
	org := actingOrg(p, append([]uuid.UUID{acc.OrganizationID}, res.LinkedOrgIDs...)...)
	if err := h.audit(ctx, p, acc.GuardianID, org, acc.PetID, "encounter", acc.ID); err != nil {
		return nil, err
	}
	body, version, err := h.view(ctx, p, acc.ID)
	if err != nil {
		return nil, err
	}
	return gen.GetEncounter200JSONResponse{Body: body, Headers: gen.GetEncounter200ResponseHeaders{ETag: &version}}, nil
}

// ---------------------------------------------------------- editar

// UpdateEncounter aplica el autoguardado de motivo/tipo/inicio, S/O y
// responsable (FE-003). Cada grupo es un comando sobre el mismo agregado; el
// If-Match se exige al primero y se encadena con la versión resultante. Un
// grupo sin cambios (NO_CHANGES) se omite.
func (h *ClinicalHandlers) UpdateEncounter(ctx context.Context, req gen.UpdateEncounterRequestObject) (gen.UpdateEncounterResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	id := uuid.UUID(req.EncounterId)
	if _, _, err := h.load(ctx, p, id, authz.EncounterEdit); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		b = &gen.UpdateEncounterRequest{}
	}
	var cmds []bus.Command
	if b.Type != nil || b.Reason != nil || b.StartedAt != nil {
		c := domain.UpdateEncounterDetails{EncounterID: id, Reason: b.Reason, StartedAt: b.StartedAt}
		if b.Type != nil {
			c.Type = ptr(string(*b.Type))
		}
		cmds = append(cmds, c)
	}
	if b.Subjective != nil || b.Objective != nil {
		c := domain.UpdateNarrative{EncounterID: id, Subjective: b.Subjective}
		if b.Objective != nil {
			c.Objective = ptr(fromObjective(*b.Objective))
		}
		cmds = append(cmds, c)
	}
	if b.AttendingVetId != nil {
		cmds = append(cmds, domain.AssignAttendingVet{EncounterID: id, UserID: uuid.UUID(*b.AttendingVetId)})
	}
	expected := int64(req.Params.IfMatch)
	for _, c := range cmds {
		r, err := h.Commands.Dispatch(h.meta(ctx, p, "", expected), c)
		switch {
		case errors.Is(err, domain.ErrNoChanges):
			continue
		case err != nil:
			return nil, mapErr(err)
		}
		expected = r.Version
	}
	body, version, err := h.view(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return gen.UpdateEncounter200JSONResponse{EncounterOKJSONResponse: ok(body, version)}, nil
}

func ok(body gen.Encounter, version int64) gen.EncounterOKJSONResponse {
	return gen.EncounterOKJSONResponse{Body: body, Headers: gen.EncounterOKResponseHeaders{ETag: &version}}
}

// PutEncounterVitals reemplaza las constantes (avisos en warnings[], PRD-005).
func (h *ClinicalHandlers) PutEncounterVitals(ctx context.Context, req gen.PutEncounterVitalsRequestObject) (gen.PutEncounterVitalsResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterEdit, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		vitals := domain.Vitals{}
		if req.Body != nil {
			vitals = fromVitals(*req.Body)
		}
		return domain.RecordVitals{EncounterID: id, Vitals: vitals}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.PutEncounterVitals200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// PutEncounterDiagnoses fija los diagnósticos (E4: veterinario).
func (h *ClinicalHandlers) PutEncounterDiagnoses(ctx context.Context, req gen.PutEncounterDiagnosesRequestObject) (gen.PutEncounterDiagnosesResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterClinicalEdit, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		var ds []domain.Diagnosis
		if req.Body != nil {
			ds = fromDiagnoses(req.Body.Diagnoses)
		}
		return domain.SetDiagnoses{EncounterID: id, Diagnoses: ds}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.PutEncounterDiagnoses200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// PutEncounterPlan fija el plan (E4: veterinario).
func (h *ClinicalHandlers) PutEncounterPlan(ctx context.Context, req gen.PutEncounterPlanRequestObject) (gen.PutEncounterPlanResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterClinicalEdit, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		plan := domain.Plan{}
		if req.Body != nil {
			plan = fromPlan(*req.Body)
		}
		return domain.SetPlan{EncounterID: id, Plan: plan}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.PutEncounterPlan200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// PutEncounterFollowUp recomienda un control (PRD-007).
func (h *ClinicalHandlers) PutEncounterFollowUp(ctx context.Context, req gen.PutEncounterFollowUpRequestObject) (gen.PutEncounterFollowUpResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterClinicalEdit, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		c := domain.RecommendFollowUp{EncounterID: id}
		if req.Body != nil {
			c.DueDate = req.Body.DueDate.Format("2006-01-02")
			c.Reason = req.Body.Reason
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.PutEncounterFollowUp200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// DeleteEncounterFollowUp retira el control recomendado.
func (h *ClinicalHandlers) DeleteEncounterFollowUp(ctx context.Context, req gen.DeleteEncounterFollowUpRequestObject) (gen.DeleteEncounterFollowUpResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterClinicalEdit, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		return domain.ClearFollowUp{EncounterID: id}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.DeleteEncounterFollowUp200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// SignEncounter firma la consulta (E5–E8). El hash lo calcula app.
func (h *ClinicalHandlers) SignEncounter(ctx context.Context, req gen.SignEncounterRequestObject) (gen.SignEncounterResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterSign, int64(req.Params.IfMatch), func(app.EncounterAccess) (bus.Command, error) {
		return domain.SignEncounter{EncounterID: id}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.SignEncounter200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// DiscardEncounter descarta un borrador (motivo obligatorio).
func (h *ClinicalHandlers) DiscardEncounter(ctx context.Context, req gen.DiscardEncounterRequestObject) (gen.DiscardEncounterResponseObject, error) {
	id := uuid.UUID(req.EncounterId)
	body, v, err := h.mutate(ctx, id, authz.EncounterDiscard, 0, func(app.EncounterAccess) (bus.Command, error) {
		c := domain.DiscardEncounter{EncounterID: id}
		if req.Body != nil {
			c.Reason = req.Body.Reason
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.DiscardEncounter200JSONResponse{EncounterOKJSONResponse: ok(body, v)}, nil
}

// ---------------------------------------------------------- adendas

// AddEncounterAddendum añade una adenda firmada (E9) con los adjuntos
// subidos previamente a la consulta firmada (attachment_ids).
func (h *ClinicalHandlers) AddEncounterAddendum(ctx context.Context, req gen.AddEncounterAddendumRequestObject) (gen.AddEncounterAddendumResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	id := uuid.UUID(req.EncounterId)
	if _, _, err := h.load(ctx, p, id, authz.EncounterSign); err != nil {
		return nil, err
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	hash := sha256.Sum256(httpserver.RawBodyFrom(ctx))
	outcome, err := h.Idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayed{status: outcome.Status, body: outcome.Body}, nil
	}
	b := req.Body
	if b == nil {
		b = &gen.AddendumRequest{}
	}
	cmd := domain.AddAddendum{EncounterID: id, Text: b.Text, Reason: string(b.Reason)}
	var ids []uuid.UUID
	if b.AttachmentIds != nil {
		for _, aid := range *b.AttachmentIds {
			s, err := h.Uploads.Get(ctx, id, uuid.UUID(aid))
			if err != nil {
				_ = h.Idem.Abort(ctx, p.UserID, key)
				return nil, domain.ErrAttachmentNotFound
			}
			cmd.Attachments = append(cmd.Attachments, s.Attachment)
			ids = append(ids, s.Attachment.ID)
		}
	}
	if _, err := h.Commands.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, mapErr(err)
	}
	if err := h.Uploads.Delete(ctx, id, ids); err != nil {
		h.Logger.WarnContext(ctx, "clinical: no se pudieron limpiar adjuntos pendientes", "encounter_id", id, "error", err)
	}
	body, version, err := h.view(ctx, p, id)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.AddEncounterAddendum201JSONResponse{EncounterCreatedJSONResponse: gen.EncounterCreatedJSONResponse{
		Body: body, Headers: gen.EncounterCreatedResponseHeaders{ETag: &version},
	}}, nil
}

// ------------------------------------------------------ verificación

// VerifyEncounter verifica el hash de una consulta firmada (pública, sin PII,
// API-013): recomputa desde es.events.
func (h *ClinicalHandlers) VerifyEncounter(ctx context.Context, req gen.VerifyEncounterRequestObject) (gen.VerifyEncounterResponseObject, error) {
	hash := ""
	if req.Params.Hash != nil {
		hash = *req.Params.Hash
		if !isHexSHA256(hash) {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "hash", Code: "INVALID"}}}
		}
	}
	v, err := h.Commands.VerifyEncounter(ctx, uuid.UUID(req.EncounterId), hash)
	if err != nil {
		return nil, err
	}
	out := gen.EncounterVerification{Valid: v.Valid, SignedAt: v.SignedAt, StreamVersion: v.StreamVersion}
	if v.Valid {
		out.ContentHash = &v.ContentHash
	}
	if v.OrganizationName != "" {
		out.OrganizationName = &v.OrganizationName
	}
	return gen.VerifyEncounter200JSONResponse(out), nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------ replay

// replayed reproduce una respuesta idempotente almacenada (ARQ-008).
type replayed struct {
	status int
	body   []byte
}

func (r replayed) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(r.status)
	_, err := w.Write(r.body)
	return err
}

func (r replayed) VisitOpenEncounterResponse(w http.ResponseWriter) error { return r.write(w) }

func (r replayed) VisitAddEncounterAddendumResponse(w http.ResponseWriter) error { return r.write(w) }

func (r replayed) VisitRecordCardEntryResponse(w http.ResponseWriter) error { return r.write(w) }

func (r replayed) VisitDeclareCardEntryResponse(w http.ResponseWriter) error { return r.write(w) }

func (r replayed) VisitRecordEncounterVaccinationResponse(w http.ResponseWriter) error {
	return r.write(w)
}

var (
	_ gen.OpenEncounterResponseObject              = replayed{}
	_ gen.AddEncounterAddendumResponseObject       = replayed{}
	_ gen.RecordCardEntryResponseObject            = replayed{}
	_ gen.DeclareCardEntryResponseObject           = replayed{}
	_ gen.RecordEncounterVaccinationResponseObject = replayed{}
)
