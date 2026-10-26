package http

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ------------------------------------------------- transferencia (PRD-010)

// InitiatePetTransfer inicia la transferencia (Idempotency-Key → 202 Pet).
func (h *PetHandlers) InitiatePetTransfer(ctx context.Context, req gen.InitiatePetTransferRequestObject) (gen.InitiatePetTransferResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetTransfer, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	raw := httpserver.RawBodyFrom(ctx)
	hash := sha256.Sum256(raw)
	outcome, err := h.idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayedResponse{status: outcome.Status, body: outcome.Body}, nil
	}
	ctx, _ = h.meta(ctx, p, key.String(), 0)
	if _, err := h.dispatch(ctx, domain.InitiateGuardianshipTransfer{PetID: view.ID, ToEmail: string(req.Body.ToEmail)}); err != nil {
		_ = h.idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	body := toPet(updated, h.presign())
	rawBody, _ := json.Marshal(body)
	if err := h.idem.Commit(ctx, p.UserID, key, http.StatusAccepted, rawBody); err != nil {
		return nil, err
	}
	return gen.InitiatePetTransfer202JSONResponse{
		Headers: gen.InitiatePetTransfer202ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    body,
	}, nil
}

// CancelPetTransfer cancela la transferencia pendiente.
func (h *PetHandlers) CancelPetTransfer(ctx context.Context, req gen.CancelPetTransferRequestObject) (gen.CancelPetTransferResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetTransfer, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	if _, err := h.dispatch(ctx, domain.CancelGuardianshipTransfer{PetID: view.ID}); err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.CancelPetTransfer200JSONResponse{
		Headers: gen.CancelPetTransfer200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// GetPetTransfer previsualiza una transferencia por token (404 sin distinguir).
func (h *PetHandlers) GetPetTransfer(ctx context.Context, req gen.GetPetTransferRequestObject) (gen.GetPetTransferResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetTransferRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	tr, err := h.queries.TransferByTokenHash(ctx, kernel.HashToken(req.Token), h.now())
	if err != nil {
		return nil, domain.ErrTransferTokenInvalid
	}
	view, err := h.queries.GetPet(ctx, tr.PetID)
	if err != nil {
		return nil, domain.ErrTransferTokenInvalid
	}
	return gen.GetPetTransfer200JSONResponse{
		PetName: view.Name, Species: gen.Species(view.Species),
		FromGuardianDisplayName: view.GuardianDisplayName, ExpiresAt: tr.ExpiresAt,
	}, nil
}

// AcceptPetTransfer acepta como el principal (email debe coincidir).
func (h *PetHandlers) AcceptPetTransfer(ctx context.Context, req gen.AcceptPetTransferRequestObject) (gen.AcceptPetTransferResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetTransferAccept, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	res, err := h.svc.AcceptTransferToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, res.AggregateID)
	if err != nil {
		return nil, err
	}
	return gen.AcceptPetTransfer200JSONResponse{
		Headers: gen.AcceptPetTransfer200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// ------------------------------------------------- vínculos (PRD-015)

// GrantCareLink vincula la mascota a una clínica activa (201; 200 si ya
// estaba activo: idempotente).
func (h *PetHandlers) GrantCareLink(ctx context.Context, req gen.GrantCareLinkRequestObject) (gen.GrantCareLinkResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetCareLinkGrant, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	orgID := uuid.UUID(req.Body.OrganizationId)
	res, err := h.dispatch(ctx, domain.GrantCareLink{PetID: view.ID, OrganizationID: orgID, ActorID: p.UserID})
	if err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	body := toPet(updated, h.presign())
	if res.Version == 0 || res.Version == view.Version {
		return gen.GrantCareLink200JSONResponse{Headers: gen.GrantCareLink200ResponseHeaders{ETag: ptr(updated.Version)}, Body: body}, nil
	}
	return gen.GrantCareLink201JSONResponse{Headers: gen.GrantCareLink201ResponseHeaders{ETag: ptr(updated.Version)}, Body: body}, nil
}

// RevokeCareLink revoca un vínculo activo.
func (h *PetHandlers) RevokeCareLink(ctx context.Context, req gen.RevokeCareLinkRequestObject) (gen.RevokeCareLinkResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetCareLinkRevoke, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	cmd := domain.RevokeCareLink{PetID: view.ID, OrganizationID: uuid.UUID(req.OrganizationId), ActorID: p.UserID}
	if req.Body != nil && req.Body.Reason != nil {
		cmd.Reason = *req.Body.Reason
	}
	if _, err := h.dispatch(ctx, cmd); err != nil {
		if errors.Is(err, domain.ErrCareLinkNotActive) {
			return nil, &kernel.NotFoundError{Resource: "care_link"}
		}
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.RevokeCareLink200JSONResponse{
		Headers: gen.RevokeCareLink200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// ------------------------------------------------- pacientes (PRD-016)

// orgAccess evalúa org:patients:list; org suspendida → 403
// ORGANIZATION_SUSPENDED; otra denegación → 403 FORBIDDEN.
func orgAccess(p auth.Principal, orgID uuid.UUID) error {
	d := authz.Can(p, authz.OrgPatientsList, authz.OrgResource{ID: orgID})
	if d.Allow {
		return nil
	}
	if d.Reason == authz.ReasonOrgSuspended {
		return &problem.DirectError{P: problem.New(problem.OrganizationSuspended, "Organización suspendida",
			"La organización está suspendida; no puede acceder a pacientes.")}
	}
	return &kernel.ForbiddenError{Reason: d.Reason}
}

// ListPatients lista los pacientes con vínculo activo de la clínica.
func (h *PetHandlers) ListPatients(ctx context.Context, req gen.ListPatientsRequestObject) (gen.ListPatientsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := orgAccess(p, orgID); err != nil {
		return nil, err
	}
	var cursor app.PatientCursor
	if req.Params.Cursor != nil {
		c, err := decodePatientCursor(*req.Params.Cursor)
		if err != nil {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "cursor", Code: "INVALID"}}}
		}
		cursor = c
	}
	q, status, limit := "", "all", 20
	if req.Params.Q != nil {
		q = *req.Params.Q
	}
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	rows, next, err := h.queries.ListPatients(ctx, orgID, q, status, cursor, limit)
	if err != nil {
		return nil, err
	}
	page := gen.PatientPage{Data: make([]gen.PatientRow, 0, len(rows))}
	pr := h.presign()
	for _, r := range rows {
		item := toListItem(app.PetListItem{
			ID: r.PetID, Name: r.Name, Species: r.Species, Breed: r.Breed, Status: r.Status,
			BirthDate: r.BirthDate, Photo: r.Photo,
		})
		item.Photo = photoDTO(ctx, pr, r.Photo)
		row := gen.PatientRow{Pet: item, LinkedAt: r.LinkedAt}
		row.Guardian.DisplayName = r.GuardianDisplayName
		if r.GuardianPhone != "" {
			row.Guardian.Phone = ptr(r.GuardianPhone)
		}
		page.Data = append(page.Data, row)
	}
	if next != nil {
		page.NextCursor = ptr(encodePatientCursor(*next))
	}
	return gen.ListPatients200JSONResponse(page), nil
}

// GetPatient devuelve la ficha (solo lectura) con contacto del tutor. Exige
// que el vínculo activo sea de ESTA organización; si no → 404.
func (h *PetHandlers) GetPatient(ctx context.Context, req gen.GetPatientRequestObject) (gen.GetPatientResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	// L restringido a esta organización (vínculo activo y org no suspendida).
	res := authz.PetResource{ID: view.ID, GuardianID: view.GuardianID}
	for _, l := range view.ActiveLinks {
		if l.OrganizationID == orgID && l.Status == string(domain.LinkActive) && !l.OrgSuspended {
			res.ActiveCareLinkOrgIDs = []uuid.UUID{orgID}
		}
	}
	res.GuardianID = uuid.Nil // la vista de clínica no se concede por ser tutor
	if d := authz.Can(p, authz.PetRead, res); !d.Allow {
		if err := orgAccess(p, orgID); err != nil {
			var direct *problem.DirectError
			if errors.As(err, &direct) {
				return nil, err // ORGANIZATION_SUSPENDED
			}
		}
		return nil, notFoundOnDeny(d)
	}
	body := toPet(view, h.presign())
	body.PendingTransfer = nil
	if d := authz.Can(p, authz.PetGuardianContact, res); d.Allow {
		body.GuardianContact = h.guardianContact(ctx, view.GuardianID)
	}
	return gen.GetPatient200JSONResponse(body), nil
}

func (h *PetHandlers) guardianContact(ctx context.Context, guardianID uuid.UUID) *gen.GuardianContact {
	if h.guardian == nil {
		return nil
	}
	c, ok := h.guardian.Contact(ctx, guardianID)
	if !ok {
		return nil
	}
	out := &gen.GuardianContact{DisplayName: c.DisplayName, Email: c.Email}
	if c.Phone != "" {
		out.Phone = ptr(c.Phone)
	}
	return out
}

func photoDTO(ctx context.Context, pr presigner, ref *domain.PhotoRef) *gen.Photo {
	if ref == nil || pr == nil {
		return nil
	}
	full, err := pr.Presign(ctx, ref.ObjectKey)
	if err != nil {
		return nil
	}
	thumb, err := pr.Presign(ctx, ref.ThumbKey)
	if err != nil {
		return nil
	}
	return &gen.Photo{Url: full, ThumbUrl: thumb, ExpiresAt: time.Now().Add(photoURLTTL)}
}

func encodePatientCursor(c app.PatientCursor) string {
	raw, _ := json.Marshal(map[string]any{"t": c.LinkedAt.UTC().Format(time.RFC3339Nano), "id": c.PetID.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePatientCursor(s string) (app.PatientCursor, error) {
	c, err := decodeCursor(s)
	if err != nil {
		return app.PatientCursor{}, err
	}
	return app.PatientCursor{LinkedAt: c.CreatedAt, PetID: c.ID}, nil
}

// replayedResponse reproduce una respuesta almacenada (ARQ-008) para los POST
// idempotentes de mascotas distintos de createPet.
type replayedResponse struct {
	status int
	body   []byte
}

func (r replayedResponse) VisitInitiatePetTransferResponse(w http.ResponseWriter) error {
	return replayedCreatePetResponse(r).VisitCreatePetResponse(w)
}

var _ gen.InitiatePetTransferResponseObject = replayedResponse{}
