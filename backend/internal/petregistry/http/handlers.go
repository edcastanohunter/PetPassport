// Package http implementa las operaciones de mascotas del contrato OpenAPI
// (PP-F1-API-004) sobre el strict server generado. Cada handler: principal →
// authz.Can con la x-pp-action del contrato → comando por el bus (con
// Idempotency-Key o If-Match) → respuesta con ETag/Location. Errores →
// problem+json vía platform/problem (ARQ-010).
package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage/image"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// photoURLTTL es la vigencia de las URLs prefirmadas (ARQ-012).
const photoURLTTL = 15 * time.Minute

// PetHandlers implementa las operaciones de mascotas del contrato. Nombre
// distinto de identityhttp.Handlers para componerlos en cmd/api.
type PetHandlers struct {
	svc      *app.Service
	queries  app.PetQueries
	idem     *idempotency.Store
	objects  storage.ObjectStorage // nil en pruebas sin storage
	guardian app.GuardianLookup    // contacto del tutor para clínicas (nil = sin contacto)
	logger   *slog.Logger
	now      func() time.Time
}

// NewPetHandlers construye los handlers.
func NewPetHandlers(svc *app.Service, queries app.PetQueries, idem *idempotency.Store, objects storage.ObjectStorage, logger *slog.Logger) *PetHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &PetHandlers{svc: svc, queries: queries, idem: idem, objects: objects, logger: logger, now: time.Now}
}

// WithGuardian inyecta el puerto de Identity para guardian_contact (RN-05).
func (h *PetHandlers) WithGuardian(g app.GuardianLookup) *PetHandlers {
	h.guardian = g
	return h
}

func (h *PetHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "sin principal en el contexto"}
	}
	return p, nil
}

// meta construye el CommandContext del request y lo adjunta al contexto (DOM-012).
func (h *PetHandlers) meta(ctx context.Context, p auth.Principal, idemKey string, expected int64) (context.Context, app.CommandContext) {
	info := reqctx.From(ctx)
	cc := app.CommandContext{
		Principal: app.PrincipalInfo{
			UserID:     p.UserID,
			Email:      p.Email,
			Roles:      p.GlobalRoles,
			SuperAdmin: p.HasRole("super_admin"),
		},
		CorrelationID:   info.CorrelationID,
		CausationID:     info.RequestID,
		Source:          "api",
		IdempotencyKey:  idemKey,
		ExpectedVersion: expected,
	}
	return app.WithCommandContext(ctx, cc), cc
}

// dispatch ejecuta el comando por el bus.
func (h *PetHandlers) dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	return h.svc.Dispatch(ctx, cmd)
}

// notFound convierte una denegación de lectura en 404 (ARQ-010: no revelar
// existencia).
func notFoundOnDeny(d authz.Decision) error {
	return &kernel.NotFoundError{Resource: "pet"}
}

// petResource carga la vista de autorización desde el read model.
func petResource(v app.PetView) authz.PetResource {
	res := authz.PetResource{ID: v.ID, GuardianID: v.GuardianID}
	for _, l := range v.ActiveLinks {
		if l.Status == string(domain.LinkActive) {
			res.ActiveCareLinkOrgIDs = append(res.ActiveCareLinkOrgIDs, l.OrganizationID)
		}
	}
	return res
}

// -------------------------------------------------------------------- GET /pets

// ListPets devuelve la página de mascotas del actor.
func (h *PetHandlers) ListPets(ctx context.Context, req gen.ListPetsRequestObject) (gen.ListPetsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	// La lista solo contiene propias: el filtro por principal ES la autorización.
	if d := authz.Can(p, authz.PetRead, authz.PetResource{GuardianID: p.UserID, ID: p.UserID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	status := "all"
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	var cursor app.PetCursor
	if req.Params.Cursor != nil {
		c, err := decodeCursor(*req.Params.Cursor)
		if err != nil {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "cursor", Code: "INVALID"}}}
		}
		cursor = c
	}
	items, next, err := h.queries.ListPets(ctx, p.UserID, status, cursor, limit)
	if err != nil {
		return nil, err
	}
	page := gen.PetPage{Data: make([]gen.PetListItem, 0, len(items))}
	for _, it := range items {
		page.Data = append(page.Data, toListItem(it))
	}
	if next != nil {
		page.NextCursor = ptr(encodeCursor(*next))
	}
	return gen.ListPets200JSONResponse{Body: page}, nil
}

// ------------------------------------------------------------------- POST /pets

// CreatePet registra una mascota (Idempotency-Key obligatoria, ARQ-008).
func (h *PetHandlers) CreatePet(ctx context.Context, req gen.CreatePetRequestObject) (gen.CreatePetResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetRegister, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	raw := httpserver.RawBodyFrom(ctx)
	hash := sha256.Sum256(raw)
	outcome, err := h.idem.Begin(ctx, p.UserID, uuid.UUID(req.Params.IdempotencyKey), hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayedCreatePetResponse{status: outcome.Status, body: outcome.Body}, nil
	}

	ctx, _ = h.meta(ctx, p, uuid.UUID(req.Params.IdempotencyKey).String(), 0)
	b := req.Body
	cmd := domain.RegisterPet{
		GuardianID: p.UserID, Name: b.Name,
		Species: string(b.Species), Sex: string(b.Sex), Neutered: string(b.Neutered),
	}
	if b.SpeciesOther != nil {
		cmd.SpeciesOther = *b.SpeciesOther
	}
	if b.Breed != nil {
		cmd.Breed = b.Breed
	}
	if b.Markings != nil {
		cmd.Markings = b.Markings
	}
	if b.BirthDate != nil {
		cmd.BirthDate = &domain.BirthDateInput{Date: b.BirthDate.Date.Format("2006-01-02"), Approximate: b.BirthDate.Approximate}
	}
	result, err := h.dispatch(ctx, cmd)
	if err != nil {
		_ = h.idem.Abort(ctx, p.UserID, uuid.UUID(req.Params.IdempotencyKey))
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, result.AggregateID)
	if err != nil {
		return nil, err
	}
	body := toPet(view, h.presign())
	rawBody, _ := json.Marshal(body)
	if err := h.idem.Commit(ctx, p.UserID, uuid.UUID(req.Params.IdempotencyKey), http.StatusCreated, rawBody); err != nil {
		return nil, err
	}
	location := "/api/v1/pets/" + result.AggregateID.String()
	return gen.CreatePet201JSONResponse{
		Headers: gen.CreatePet201ResponseHeaders{ETag: ptr(int64(view.Version)), Location: &location},
		Body:    body,
	}, nil
}

// ------------------------------------------------------------------ GET /pets/{id}

// GetPet devuelve la ficha. Denegación → 404 (no revelar existencia).
func (h *PetHandlers) GetPet(ctx context.Context, req gen.GetPetRequestObject) (gen.GetPetResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err // NotFoundError → 404
	}
	res := petResource(view)
	if d := authz.Can(p, authz.PetRead, res); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	body := toPet(view, h.presign())
	if p.UserID != view.GuardianID {
		// Vista de clínica vinculada o super_admin: sin transferencia pendiente;
		// contacto del tutor solo con pet:guardian_contact:read (API-004).
		body.PendingTransfer = nil
		if d := authz.Can(p, authz.PetGuardianContact, res); d.Allow {
			body.GuardianContact = h.guardianContact(ctx, view.GuardianID)
		}
	}
	return gen.GetPet200JSONResponse{
		Headers: gen.GetPet200ResponseHeaders{ETag: ptr(int64(view.Version))},
		Body:    body,
	}, nil
}

// ------------------------------------------------------------------ PATCH /pets/{id}

// UpdatePet edita el perfil (If-Match obligatorio).
func (h *PetHandlers) UpdatePet(ctx context.Context, req gen.UpdatePetRequestObject) (gen.UpdatePetResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetUpdate, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", int64(req.Params.IfMatch))
	b := req.Body
	cmd := domain.UpdatePetProfile{PetID: view.ID}
	if b.Name != nil {
		cmd.Name = b.Name
	}
	if b.Species != nil {
		s := string(*b.Species)
		cmd.Species = &s
	}
	if b.SpeciesOther != nil {
		cmd.SpeciesOther = b.SpeciesOther
	}
	if b.Breed != nil {
		cmd.Breed = b.Breed
	}
	if b.Sex != nil {
		s := string(*b.Sex)
		cmd.Sex = &s
	}
	if b.Neutered != nil {
		s := string(*b.Neutered)
		cmd.Neutered = &s
	}
	if b.BirthDate != nil {
		cmd.BirthDate = &domain.BirthDateInput{Date: b.BirthDate.Date.Format("2006-01-02"), Approximate: b.BirthDate.Approximate}
	}
	if b.Markings != nil {
		cmd.Markings = b.Markings
	}
	result, err := h.dispatch(ctx, cmd)
	if err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, result.AggregateID)
	if err != nil {
		return nil, err
	}
	return gen.UpdatePet200JSONResponse{
		Headers: gen.UpdatePet200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// ------------------------------------------------------- PUT /pets/{id}/microchip

// PutPetMicrochip asigna o corrige el microchip (If-Match).
func (h *PetHandlers) PutPetMicrochip(ctx context.Context, req gen.PutPetMicrochipRequestObject) (gen.PutPetMicrochipResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetMicrochip, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", int64(req.Params.IfMatch))
	result, err := h.dispatch(ctx, domain.AssignMicrochip{PetID: view.ID, Microchip: req.Body.Microchip})
	if err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, result.AggregateID)
	if err != nil {
		return nil, err
	}
	return gen.PutPetMicrochip200JSONResponse{
		Headers: gen.PutPetMicrochip200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// ------------------------------------------------------------ PUT/DELETE photo

// PutPetPhoto sube la foto: pipeline de imagen (ARQ-012) + comando.
func (h *PetHandlers) PutPetPhoto(ctx context.Context, req gen.PutPetPhotoRequestObject) (gen.PutPetPhotoResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetPhoto, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	if h.objects == nil {
		return nil, errors.New("photo: storage no configurado")
	}
	file, err := firstFile(req.Body)
	if err != nil {
		return nil, photoProblem(err)
	}
	res, err := image.Process(file)
	if err != nil {
		return nil, photoProblem(err)
	}
	objectID := kernel.NewID().String()
	fullKey := storage.PhotoKey(view.ID.String(), objectID, ".jpeg")
	thumbKey := storage.PhotoKey(view.ID.String(), objectID+"_t", ".jpeg")
	if err := h.objects.Put(ctx, fullKey, res.ContentType, bytesReader(res.Full), int64(len(res.Full))); err != nil {
		return nil, err
	}
	if err := h.objects.Put(ctx, thumbKey, res.ContentType, bytesReader(res.Thumb), int64(len(res.Thumb))); err != nil {
		_ = h.objects.Delete(ctx, fullKey)
		return nil, err
	}
	ref := domain.PhotoRef{ObjectKey: fullKey, ThumbKey: thumbKey, ContentType: "jpeg"}
	ctx, _ = h.meta(ctx, p, "", 0)
	if _, err := h.dispatch(ctx, domain.ChangePetPhoto{PetID: view.ID, Photo: &ref}); err != nil {
		// Limpieza best-effort de los objetos huérfanos.
		_ = h.objects.Delete(ctx, fullKey)
		_ = h.objects.Delete(ctx, thumbKey)
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.PutPetPhoto200JSONResponse{
		Headers: gen.PutPetPhoto200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// DeletePetPhoto quita la foto.
func (h *PetHandlers) DeletePetPhoto(ctx context.Context, req gen.DeletePetPhotoRequestObject) (gen.DeletePetPhotoResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetPhoto, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	if _, err := h.dispatch(ctx, domain.ChangePetPhoto{PetID: view.ID}); err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.DeletePetPhoto200JSONResponse{
		Headers: gen.DeletePetPhoto200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// ------------------------------------------------------------- deceased + undo

// MarkPetDeceased marca la mascota fallecida (If-Match).
func (h *PetHandlers) MarkPetDeceased(ctx context.Context, req gen.MarkPetDeceasedRequestObject) (gen.MarkPetDeceasedResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetDeceased, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", int64(req.Params.IfMatch))
	cmd := domain.MarkPetDeceased{PetID: view.ID}
	if req.Body != nil && req.Body.DeceasedAt != nil {
		d := req.Body.DeceasedAt.Format("2006-01-02")
		cmd.DeceasedAt = &d
	}
	if _, err := h.dispatch(ctx, cmd); err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.MarkPetDeceased200JSONResponse{
		Headers: gen.MarkPetDeceased200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// UndoPetDeceased deshace el fallecimiento dentro de la ventana (PRD-009).
func (h *PetHandlers) UndoPetDeceased(ctx context.Context, req gen.UndoPetDeceasedRequestObject) (gen.UndoPetDeceasedResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetDeceased, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	within := false
	if view.DeceasedMarkedAt != nil {
		within = h.now().Sub(*view.DeceasedMarkedAt) < domain.UndoWindow
	}
	ctx, _ = h.meta(ctx, p, "", 0)
	if _, err := h.dispatch(ctx, domain.UndoDeceased{
		PetID: view.ID, WithinUndoWindow: within, ActorIsSuperAdmin: p.HasRole("super_admin"),
	}); err != nil {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.UndoPetDeceased200JSONResponse{
		Headers: gen.UndoPetDeceased200ResponseHeaders{ETag: ptr(int64(updated.Version))},
		Body:    toPet(updated, h.presign()),
	}, nil
}
