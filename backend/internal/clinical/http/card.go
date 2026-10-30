package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CardCommands es la fachada de escritura del carnet (app.CardService).
type CardCommands interface {
	Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error)
	Propose(ctx context.Context, petID uuid.UUID, in domain.EntryInput) (domain.Proposal, error)
}

// CardDeps son las dependencias del carnet, catálogos y enlaces compartidos.
type CardDeps struct {
	Card          CardCommands
	CardQueries   app.CardQueries
	Catalog       app.Catalog
	CatalogAdmin  app.CatalogAdmin
	Shares        *app.ShareService
	CardDocs      CardDocuments
	PublicBaseURL string
}

// CardDocuments crea (o recupera) el trabajo de PDF de la versión vigente
// del carnet.
type CardDocuments interface {
	EnsureCardPDF(ctx context.Context, petID uuid.UUID) (app.DocumentJobView, error)
}

const dateLayout = "2006-01-02"

func toDate(s string) openapi_types.Date {
	t, _ := time.Parse(dateLayout, s)
	return openapi_types.Date{Time: t}
}

func optDate(s *string) *openapi_types.Date {
	if s == nil {
		return nil
	}
	d := toDate(*s)
	return &d
}

func dateStr(d openapi_types.Date) string { return d.Format(dateLayout) }

func optDateStr(d *openapi_types.Date) *string {
	if d == nil {
		return nil
	}
	s := dateStr(*d)
	return &s
}

func uuidPtr(id *openapi_types.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	u := uuid.UUID(*id)
	return &u
}

func optUUID(id *uuid.UUID) *openapi_types.UUID {
	if id == nil {
		return nil
	}
	u := openapi_types.UUID(*id)
	return &u
}

func toCardEntry(v app.CardEntryView) gen.CardEntry {
	out := gen.CardEntry{
		Id: openapi_types.UUID(v.ID), Kind: gen.CardEntryKind(v.Kind),
		Product:        gen.CardProduct{CatalogId: optUUID(v.CatalogID), Name: v.ProductName},
		AdministeredAt: toDate(v.AdministeredAt), Lot: v.Lot, Manufacturer: v.Manufacturer, ExpiryDate: optDate(v.ExpiryDate),
		Route: v.Route, Site: v.Site, DoseLabel: v.DoseLabel, WeightKg: f32(v.WeightKg),
		Source: gen.CardEntrySource(v.Source), Verification: gen.CardEntryVerification(v.Verification),
		Status: gen.CardEntryStatus(v.Status), VoidReason: v.VoidReason, RecordedByOrganizationId: optUUID(v.RecordedByOrg),
		EncounterId: optUUID(v.EncounterID), HasEvidence: v.HasEvidence,
	}
	if v.NextDueDate != nil {
		out.NextDue = &gen.CardNextDue{Date: toDate(*v.NextDueDate), Basis: gen.CardNextDueBasis(deref(v.NextDueBasis))}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toCard(c app.CardView) gen.Card {
	out := gen.Card{
		PetId: openapi_types.UUID(c.PetID), Species: c.Species,
		Entries: make([]gen.CardEntry, 0, len(c.Entries)), NextDue: make([]gen.NextDueItem, 0, len(c.NextDue)),
	}
	for _, e := range c.Entries {
		out.Entries = append(out.Entries, toCardEntry(e))
	}
	for _, n := range c.NextDue {
		out.NextDue = append(out.NextDue, gen.NextDueItem{
			EntryId: openapi_types.UUID(n.EntryID), Kind: gen.CardEntryKind(n.Kind), ProductName: n.ProductName,
			DueDate: toDate(n.DueDate), Basis: n.Basis, Status: gen.NextDueItemStatus(n.Status),
		})
	}
	return out
}

// cardAccess autoriza una acción sobre el carnet de la mascota. No visible →
// 404; visible pero sin permiso → 403. Devuelve la organización con la que
// actúa el principal (miembro de una org vinculada) o nil.
func (h *ClinicalHandlers) cardAccess(ctx context.Context, p auth.Principal, petID uuid.UUID, action authz.Action) (*uuid.UUID, uuid.UUID, error) {
	guardianID, err := h.Pets.GuardianOf(ctx, petID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	linked, err := h.Pets.LinkedOrgIDs(ctx, petID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	custody, err := h.Pets.CustodyOrgIDs(ctx, petID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	res := authz.PetRecordResource{PetID: petID, GuardianID: guardianID, LinkedOrgIDs: linked, CustodyOrgIDs: custody}
	if !authz.RecordVisible(p, res) {
		return nil, uuid.Nil, &kernel.NotFoundError{Resource: "pet"}
	}
	if d := authz.Can(p, action, res); !d.Allow {
		return nil, uuid.Nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	return actingOrg(p, linked...), guardianID, nil
}

// GetPetCard devuelve el carnet (vista síncrona).
func (h *ClinicalHandlers) GetPetCard(ctx context.Context, req gen.GetPetCardRequestObject) (gen.GetPetCardResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	if _, err := h.readRecord(ctx, petID); err != nil {
		return nil, err
	}
	card, err := h.CardQueries.Card(ctx, petID, true)
	if err != nil {
		return nil, err
	}
	return gen.GetPetCard200JSONResponse(toCard(card)), nil
}

func entryInput(kind gen.CardEntryKind, product gen.CardProduct, at openapi_types.Date, lot, dose *string, next *openapi_types.Date) domain.EntryInput {
	return domain.EntryInput{
		Kind: string(kind), Product: domain.ProductRef{CatalogID: uuidPtr(product.CatalogId), Name: product.Name},
		AdministeredAt: dateStr(at), Lot: lot, DoseLabel: dose, NextDueDate: optDateStr(next),
	}
}

// recordEntry es el cuerpo común de POST /card/entries y del atajo desde la
// consulta (encounterID != nil).
func (h *ClinicalHandlers) recordEntry(ctx context.Context, p auth.Principal, petID uuid.UUID, org *uuid.UUID,
	b gen.RecordCardEntryRequest, encounterID *uuid.UUID, key uuid.UUID) (gen.CardEntry, error) {
	cmd := domain.RecordEntry{
		PetID: petID, EntryID: uuid.Must(uuid.NewV7()),
		EntryInput:   entryInput(b.Kind, b.Product, b.AdministeredAt, b.Lot, b.DoseLabel, b.NextDueDate),
		Manufacturer: b.Manufacturer, ExpiryDate: optDateStr(b.ExpiryDate), Route: b.Route, Site: b.Site,
		WeightKg: f64(b.WeightKg), EncounterID: encounterID,
	}
	if encounterID == nil {
		cmd.EncounterID = uuidPtr(b.EncounterId)
	}
	if org != nil {
		cmd.Actor.OrganizationID = *org
	}
	if _, err := h.Card.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		return gen.CardEntry{}, mapErr(err)
	}
	v, err := h.CardQueries.Entry(ctx, petID, cmd.EntryID)
	if err != nil {
		return gen.CardEntry{}, err
	}
	return toCardEntry(v), nil
}

// RecordCardEntry registra una vacuna o preventivo (idempotente).
func (h *ClinicalHandlers) RecordCardEntry(ctx context.Context, req gen.RecordCardEntryRequestObject) (gen.RecordCardEntryResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	org, _, err := h.cardAccess(ctx, p, petID, authz.CardRecord)
	if err != nil {
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
	if req.Body == nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	entry, err := h.recordEntry(ctx, p, petID, org, *req.Body, nil, key)
	if err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	raw, _ := json.Marshal(entry)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.RecordCardEntry201JSONResponse(entry), nil
}

// RecordEncounterVaccination es el atajo desde la consulta (PRD-010): enlaza
// la entrada al encuentro en borrador de la organización del miembro.
func (h *ClinicalHandlers) RecordEncounterVaccination(ctx context.Context, req gen.RecordEncounterVaccinationRequestObject) (gen.RecordEncounterVaccinationResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	encID := uuid.UUID(req.EncounterId)
	enc, _, err := h.load(ctx, p, encID, authz.EncounterEdit)
	if err != nil {
		return nil, err
	}
	if enc.Status != "draft" {
		return nil, domain.ErrEncounterNotDraft
	}
	org, _, err := h.cardAccess(ctx, p, enc.PetID, authz.CardRecord)
	if err != nil {
		return nil, err
	}
	if org == nil || *org != enc.OrganizationID {
		return nil, &kernel.ForbiddenError{Reason: "la organización del encuentro no es la del actor"}
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
	if req.Body == nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	entry, err := h.recordEntry(ctx, p, enc.PetID, org, *req.Body, &encID, key)
	if err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	raw, _ := json.Marshal(entry)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.RecordEncounterVaccination201JSONResponse(entry), nil
}

// DeclareCardEntry es la declaración del tutor (idempotente).
func (h *ClinicalHandlers) DeclareCardEntry(ctx context.Context, req gen.DeclareCardEntryRequestObject) (gen.DeclareCardEntryResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if _, _, err := h.cardAccess(ctx, p, petID, authz.CardDeclare); err != nil {
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
	if req.Body == nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	b := req.Body
	cmd := domain.DeclareEntry{
		PetID: petID, EntryID: uuid.Must(uuid.NewV7()), EvidenceKey: b.EvidenceObjectKey,
		EntryInput: entryInput(b.Kind, b.Product, b.AdministeredAt, b.Lot, b.DoseLabel, nil),
	}
	if _, err := h.Card.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, mapErr(err)
	}
	v, err := h.CardQueries.Entry(ctx, petID, cmd.EntryID)
	if err != nil {
		return nil, err
	}
	entry := toCardEntry(v)
	raw, _ := json.Marshal(entry)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.DeclareCardEntry201JSONResponse(entry), nil
}

// entryCommand ejecuta un comando sobre una entrada y devuelve su estado.
func (h *ClinicalHandlers) entryCommand(ctx context.Context, petID, entryID uuid.UUID, action authz.Action,
	build func(org *uuid.UUID) bus.Command) (gen.CardEntry, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return gen.CardEntry{}, err
	}
	org, _, err := h.cardAccess(ctx, p, petID, action)
	if err != nil {
		return gen.CardEntry{}, err
	}
	// La entrada debe pertenecer a la mascota (404 si no).
	if _, err := h.CardQueries.Entry(ctx, petID, entryID); err != nil {
		return gen.CardEntry{}, err
	}
	cctx := app.WithPet(h.meta(ctx, p, reqctx.From(ctx).RequestID, 0), petID)
	if _, err := h.Card.Dispatch(cctx, build(org)); err != nil {
		return gen.CardEntry{}, mapErr(err)
	}
	v, err := h.CardQueries.Entry(ctx, petID, entryID)
	if err != nil {
		return gen.CardEntry{}, err
	}
	return toCardEntry(v), nil
}

func actorFor(org *uuid.UUID) domain.CardActor {
	var a domain.CardActor
	if org != nil {
		a.OrganizationID = *org
	}
	return a
}

// VerifyCardEntry verifica una entrada declarada.
func (h *ClinicalHandlers) VerifyCardEntry(ctx context.Context, req gen.VerifyCardEntryRequestObject) (gen.VerifyCardEntryResponseObject, error) {
	var note *string
	if req.Body != nil {
		note = req.Body.Note
	}
	e, err := h.entryCommand(ctx, uuid.UUID(req.PetId), uuid.UUID(req.EntryId), authz.CardVerify, func(org *uuid.UUID) bus.Command {
		return domain.VerifyEntry{EntryID: uuid.UUID(req.EntryId), Note: note, Actor: actorFor(org)}
	})
	if err != nil {
		return nil, err
	}
	return gen.VerifyCardEntry200JSONResponse(e), nil
}

// DisputeCardEntry disputa una entrada declarada.
func (h *ClinicalHandlers) DisputeCardEntry(ctx context.Context, req gen.DisputeCardEntryRequestObject) (gen.DisputeCardEntryResponseObject, error) {
	var note *string
	if req.Body != nil {
		note = req.Body.Note
	}
	e, err := h.entryCommand(ctx, uuid.UUID(req.PetId), uuid.UUID(req.EntryId), authz.CardVerify, func(org *uuid.UUID) bus.Command {
		return domain.DisputeEntry{EntryID: uuid.UUID(req.EntryId), Note: note, Actor: actorFor(org)}
	})
	if err != nil {
		return nil, err
	}
	return gen.DisputeCardEntry200JSONResponse(e), nil
}

// VoidCardEntry anula una entrada (V4, V5).
func (h *ClinicalHandlers) VoidCardEntry(ctx context.Context, req gen.VoidCardEntryRequestObject) (gen.VoidCardEntryResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	e, err := h.entryCommand(ctx, uuid.UUID(req.PetId), uuid.UUID(req.EntryId), authz.CardVoid, func(org *uuid.UUID) bus.Command {
		return domain.VoidEntry{EntryID: uuid.UUID(req.EntryId), Reason: reason, Actor: actorFor(org)}
	})
	if err != nil {
		return nil, err
	}
	return gen.VoidCardEntry200JSONResponse(e), nil
}

// AdjustCardNextDue fija la próxima dosis (basis clinic).
func (h *ClinicalHandlers) AdjustCardNextDue(ctx context.Context, req gen.AdjustCardNextDueRequestObject) (gen.AdjustCardNextDueResponseObject, error) {
	date := ""
	if req.Body != nil {
		date = dateStr(req.Body.Date)
	}
	e, err := h.entryCommand(ctx, uuid.UUID(req.PetId), uuid.UUID(req.EntryId), authz.CardRecord, func(org *uuid.UUID) bus.Command {
		return domain.AdjustNextDue{EntryID: uuid.UUID(req.EntryId), Date: date, Actor: actorFor(org)}
	})
	if err != nil {
		return nil, err
	}
	return gen.AdjustCardNextDue200JSONResponse(e), nil
}

// GetCardNextDueProposal previsualiza el cálculo del protocolo.
func (h *ClinicalHandlers) GetCardNextDueProposal(ctx context.Context, req gen.GetCardNextDueProposalRequestObject) (gen.GetCardNextDueProposalResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if _, _, err := h.cardAccess(ctx, p, petID, authz.CardRecord); err != nil {
		return nil, err
	}
	in := domain.EntryInput{
		Kind: string(req.Params.Kind), AdministeredAt: dateStr(req.Params.AdministeredAt),
		Product: domain.ProductRef{CatalogID: uuidPtr(req.Params.ProductCatalogId), Name: "-"},
	}
	pr, err := h.Card.Propose(ctx, petID, in)
	if err != nil {
		return nil, err
	}
	out := gen.NextDueProposal{Explanation: pr.Explanation}
	if pr.NextDue != nil {
		d := toDate(pr.NextDue.Date.String())
		b := string(pr.NextDue.Basis)
		out.Date, out.Basis = &d, &b
	}
	return gen.GetCardNextDueProposal200JSONResponse(out), nil
}

// ------------------------------------------------------------- catálogos

func toVaccine(v app.VaccineRef) gen.Vaccine {
	return gen.Vaccine{Id: openapi_types.UUID(v.ID), Name: v.Name, Species: v.Species, Diseases: v.Diseases,
		Type: gen.VaccineType(v.Type), Routes: v.Routes, Active: v.Active}
}

func toProtocol(p app.ProtocolRef) gen.Protocol {
	series := make([]gen.SeriesDose, 0, len(p.InitialSeries))
	for _, s := range p.InitialSeries {
		a, i := s.MinAgeWeeks, s.MinIntervalDays
		series = append(series, gen.SeriesDose{MinAgeWeeks: &a, MinIntervalDays: &i})
	}
	out := gen.Protocol{Id: openapi_types.UUID(p.ID), Country: p.Country, Species: p.Species,
		VaccineId: openapi_types.UUID(p.VaccineID), InitialSeries: series, BoosterIntervalMonths: p.BoosterIntervalMonths,
		LegalValidityMonths: p.LegalValidityMonths, Active: p.Active}
	if p.Notes != "" {
		out.Notes = &p.Notes
	}
	return out
}

// ListVaccineCatalog lista las vacunas activas.
func (h *ClinicalHandlers) ListVaccineCatalog(ctx context.Context, req gen.ListVaccineCatalogRequestObject) (gen.ListVaccineCatalogResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	species, q := "", ""
	if req.Params.Species != nil {
		species = *req.Params.Species
	}
	if req.Params.Q != nil {
		q = strings.TrimSpace(*req.Params.Q)
	}
	list, err := h.Catalog.Vaccines(ctx, species, q)
	if err != nil {
		return nil, err
	}
	out := gen.VaccineList{Data: make([]gen.Vaccine, 0, len(list))}
	for _, v := range list {
		out.Data = append(out.Data, toVaccine(v))
	}
	return gen.ListVaccineCatalog200JSONResponse(out), nil
}

// ListVaccinationProtocols lista los protocolos activos.
func (h *ClinicalHandlers) ListVaccinationProtocols(ctx context.Context, req gen.ListVaccinationProtocolsRequestObject) (gen.ListVaccinationProtocolsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	country, species := "", ""
	if req.Params.Country != nil {
		country = *req.Params.Country
	}
	if req.Params.Species != nil {
		species = *req.Params.Species
	}
	list, err := h.Catalog.Protocols(ctx, country, species)
	if err != nil {
		return nil, err
	}
	out := gen.ProtocolList{Data: make([]gen.Protocol, 0, len(list))}
	for _, p := range list {
		out.Data = append(out.Data, toProtocol(p))
	}
	return gen.ListVaccinationProtocols200JSONResponse(out), nil
}

// adminActor autoriza admin:catalog:write y devuelve el id del actor.
func (h *ClinicalHandlers) adminActor(ctx context.Context) (uuid.UUID, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if d := authz.Can(p, authz.AdminCatalogWrite, nil); !d.Allow {
		return uuid.Nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	return p.UserID, nil
}

func validVaccine(b gen.VaccineWriteRequest) error {
	ve := &kernel.ValidationError{}
	if n := strings.TrimSpace(b.Name); n == "" || len(n) > 120 {
		ve.Add("name", "INVALID")
	}
	if len(b.Species) == 0 {
		ve.Add("species", "REQUIRED")
	}
	if strings.TrimSpace(b.Reason) == "" {
		ve.Add("reason", "REQUIRED")
	}
	return ve.ErrOrNil()
}

func vaccineOf(b gen.VaccineWriteRequest) app.VaccineRef {
	return app.VaccineRef{Name: strings.TrimSpace(b.Name), Species: b.Species, Diseases: b.Diseases, Type: string(b.Type), Routes: b.Routes}
}

// CreateVaccine crea una vacuna (super_admin, auditado).
func (h *ClinicalHandlers) CreateVaccine(ctx context.Context, req gen.CreateVaccineRequestObject) (gen.CreateVaccineResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	if err := validVaccine(*req.Body); err != nil {
		return nil, err
	}
	v, err := h.CatalogAdmin.SaveVaccine(ctx, actor, nil, vaccineOf(*req.Body), req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return gen.CreateVaccine201JSONResponse(toVaccine(v)), nil
}

// UpdateVaccine actualiza una vacuna.
func (h *ClinicalHandlers) UpdateVaccine(ctx context.Context, req gen.UpdateVaccineRequestObject) (gen.UpdateVaccineResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	if err := validVaccine(*req.Body); err != nil {
		return nil, err
	}
	id := uuid.UUID(req.VaccineId)
	v, err := h.CatalogAdmin.SaveVaccine(ctx, actor, &id, vaccineOf(*req.Body), req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return gen.UpdateVaccine200JSONResponse(toVaccine(v)), nil
}

// DeactivateVaccine da de baja una vacuna.
func (h *ClinicalHandlers) DeactivateVaccine(ctx context.Context, req gen.DeactivateVaccineRequestObject) (gen.DeactivateVaccineResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Reason) == "" {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reason", Code: "REQUIRED"}}}
	}
	if err := h.CatalogAdmin.DeactivateVaccine(ctx, actor, uuid.UUID(req.VaccineId), req.Body.Reason); err != nil {
		return nil, err
	}
	return gen.DeactivateVaccine204Response{}, nil
}

func validProtocol(b gen.ProtocolWriteRequest) error {
	ve := &kernel.ValidationError{}
	if strings.TrimSpace(b.Species) == "" {
		ve.Add("species", "REQUIRED")
	}
	if len(b.InitialSeries) == 0 {
		ve.Add("initial_series", "REQUIRED")
	}
	if strings.TrimSpace(b.Reason) == "" {
		ve.Add("reason", "REQUIRED")
	}
	return ve.ErrOrNil()
}

func protocolOf(b gen.ProtocolWriteRequest) app.ProtocolRef {
	series := make([]domain.SeriesDose, 0, len(b.InitialSeries))
	for _, s := range b.InitialSeries {
		d := domain.SeriesDose{}
		if s.MinAgeWeeks != nil {
			d.MinAgeWeeks = *s.MinAgeWeeks
		}
		if s.MinIntervalDays != nil {
			d.MinIntervalDays = *s.MinIntervalDays
		}
		series = append(series, d)
	}
	p := app.ProtocolRef{Country: b.Country, Species: b.Species, VaccineID: uuid.UUID(b.VaccineId), InitialSeries: series,
		BoosterIntervalMonths: b.BoosterIntervalMonths, LegalValidityMonths: b.LegalValidityMonths}
	if b.Notes != nil {
		p.Notes = *b.Notes
	}
	return p
}

// CreateVaccinationProtocol crea un protocolo.
func (h *ClinicalHandlers) CreateVaccinationProtocol(ctx context.Context, req gen.CreateVaccinationProtocolRequestObject) (gen.CreateVaccinationProtocolResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	if err := validProtocol(*req.Body); err != nil {
		return nil, err
	}
	p, err := h.CatalogAdmin.SaveProtocol(ctx, actor, nil, protocolOf(*req.Body), req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return gen.CreateVaccinationProtocol201JSONResponse(toProtocol(p)), nil
}

// UpdateVaccinationProtocol actualiza un protocolo.
func (h *ClinicalHandlers) UpdateVaccinationProtocol(ctx context.Context, req gen.UpdateVaccinationProtocolRequestObject) (gen.UpdateVaccinationProtocolResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	if err := validProtocol(*req.Body); err != nil {
		return nil, err
	}
	id := uuid.UUID(req.ProtocolId)
	p, err := h.CatalogAdmin.SaveProtocol(ctx, actor, &id, protocolOf(*req.Body), req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return gen.UpdateVaccinationProtocol200JSONResponse(toProtocol(p)), nil
}

// DeactivateVaccinationProtocol da de baja un protocolo.
func (h *ClinicalHandlers) DeactivateVaccinationProtocol(ctx context.Context, req gen.DeactivateVaccinationProtocolRequestObject) (gen.DeactivateVaccinationProtocolResponseObject, error) {
	actor, err := h.adminActor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Reason) == "" {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reason", Code: "REQUIRED"}}}
	}
	if err := h.CatalogAdmin.DeactivateProtocol(ctx, actor, uuid.UUID(req.ProtocolId), req.Body.Reason); err != nil {
		return nil, err
	}
	return gen.DeactivateVaccinationProtocol204Response{}, nil
}

// -------------------------------------------------------------- enlaces

func toShare(v app.ShareView) gen.Share {
	return gen.Share{Id: openapi_types.UUID(v.ID), Scope: gen.ShareScope(v.Scope), HasPin: v.HasPIN,
		ExpiresAt: v.ExpiresAt, CreatedAt: v.CreatedAt, AccessCount: v.AccessCount, LastAccessAt: v.LastAccess}
}

// ListPetShares lista los enlaces activos (sin token).
func (h *ClinicalHandlers) ListPetShares(ctx context.Context, req gen.ListPetSharesRequestObject) (gen.ListPetSharesResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if _, _, err := h.cardAccess(ctx, p, petID, authz.ShareManage); err != nil {
		return nil, err
	}
	list, err := h.Shares.List(ctx, petID)
	if err != nil {
		return nil, err
	}
	out := gen.ShareList{Data: make([]gen.Share, 0, len(list))}
	for _, s := range list {
		out.Data = append(out.Data, toShare(s))
	}
	return gen.ListPetShares200JSONResponse(out), nil
}

// CreatePetShare crea un enlace; el token solo se devuelve aquí. No es
// idempotente por respuesta (el token no se guarda en claro): la clave
// protege contra doble envío rechazando la repetición.
func (h *ClinicalHandlers) CreatePetShare(ctx context.Context, req gen.CreatePetShareRequestObject) (gen.CreatePetShareResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if _, _, err := h.cardAccess(ctx, p, petID, authz.ShareManage); err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	hash := sha256.Sum256(httpserver.RawBodyFrom(ctx))
	outcome, err := h.Idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		// El token no se puede repetir: la repetición del mismo envío es un conflicto.
		return nil, kernel.NewDomainError("SHARE_ALREADY_CREATED", "Este enlace ya se creó; el token solo se muestra una vez.")
	}
	pin := ""
	if req.Body.Pin != nil {
		pin = *req.Body.Pin
	}
	created, err := h.Shares.Create(ctx, petID, p.UserID, string(req.Body.Scope), req.Body.ExpiresInDays, pin)
	if err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusConflict, []byte(`{"code":"SHARE_ALREADY_CREATED"}`)); err != nil {
		return nil, err
	}
	return gen.CreatePetShare201JSONResponse{
		Id: openapi_types.UUID(created.ID), ExpiresAt: created.ExpiresAt,
		Url: strings.TrimRight(h.PublicBaseURL, "/") + "/share/" + created.Token,
	}, nil
}

// RevokePetShare revoca un enlace.
func (h *ClinicalHandlers) RevokePetShare(ctx context.Context, req gen.RevokePetShareRequestObject) (gen.RevokePetShareResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if _, _, err := h.cardAccess(ctx, p, petID, authz.ShareManage); err != nil {
		return nil, err
	}
	if err := h.Shares.Revoke(ctx, petID, uuid.UUID(req.ShareId)); err != nil {
		return nil, err
	}
	return gen.RevokePetShare204Response{}, nil
}

// GetSharedRecord es la vista pública (sin autenticación, sin datos del tutor).
func (h *ClinicalHandlers) GetSharedRecord(ctx context.Context, req gen.GetSharedRecordRequestObject) (gen.GetSharedRecordResponseObject, error) {
	unlock := ""
	if req.Params.XPPShareUnlock != nil {
		unlock = *req.Params.XPPShareUnlock
	}
	view, shareID, err := h.Shares.View(ctx, req.Token, unlock)
	if err != nil {
		return nil, err
	}
	h.auditShareAccess(ctx, shareID, view.Card.PetID)
	pet := gen.SharedPet{Name: view.Pet.Name, Species: view.Pet.Species, Breed: view.Pet.Breed, Sex: view.Pet.Sex,
		BirthDate: optDate(view.Pet.BirthDate), MicrochipMasked: view.Pet.MicrochipMasked}
	return gen.GetSharedRecord200JSONResponse{
		Scope: gen.SharedRecordScope(view.Scope), Pet: pet, Card: toCard(view.Card), VerifiedAt: view.VerifiedAt,
	}, nil
}

// auditShareAccess registra share.access con ip_hash (SEC-002); un fallo de
// auditoría no bloquea la lectura pública pero se registra en el log.
func (h *ClinicalHandlers) auditShareAccess(ctx context.Context, shareID, petID uuid.UUID) {
	if h.Auditor == nil {
		return
	}
	info := reqctx.From(ctx)
	if err := h.Auditor.RecordRead(ctx, app.ReadAudit{
		ActorID: uuid.Nil, ActorRoles: []string{"anonymous"}, PetID: petID, Resource: "share", ResourceID: shareID,
		Reason: "share.access", CorrelationID: info.CorrelationID,
	}); err != nil {
		h.Logger.WarnContext(ctx, "clinical: no se pudo auditar el acceso a un enlace", "error", err)
	}
}

// UnlockSharedRecord valida el PIN y devuelve el token de sesión de enlace.
func (h *ClinicalHandlers) UnlockSharedRecord(ctx context.Context, req gen.UnlockSharedRecordRequestObject) (gen.UnlockSharedRecordResponseObject, error) {
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "pin", Code: "REQUIRED"}}}
	}
	token, exp, err := h.Shares.Unlock(ctx, req.Token, req.Body.Pin)
	if err != nil {
		return nil, err
	}
	return gen.UnlockSharedRecord200JSONResponse{Token: token, ExpiresAt: exp}, nil
}

// VerifyCard verifica públicamente que el carnet coincide con el hash del QR.
func (h *ClinicalHandlers) VerifyCard(ctx context.Context, req gen.VerifyCardRequestObject) (gen.VerifyCardResponseObject, error) {
	card, err := h.CardQueries.Card(ctx, uuid.UUID(req.PetId), false)
	if err != nil {
		return nil, err
	}
	if len(card.Entries) == 0 {
		return nil, &kernel.NotFoundError{Resource: "card"}
	}
	now := h.now().UTC()
	return gen.VerifyCard200JSONResponse{
		Valid:   strings.EqualFold(app.CardFingerprint(card.Entries), req.Params.H),
		Entries: len(card.Entries), VerifiedAt: &now,
	}, nil
}

// GetPetCardPdf entrega el PDF del carnet con QR (PRD-015): 302/200 con URL
// prefirmada si está listo o 202 con el job mientras se genera.
func (h *ClinicalHandlers) GetPetCardPdf(ctx context.Context, req gen.GetPetCardPdfRequestObject) (gen.GetPetCardPdfResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	if _, err := h.readRecord(ctx, petID); err != nil {
		return nil, err
	}
	if h.CardDocs == nil {
		return nil, storageUnavailable()
	}
	job, err := h.CardDocs.EnsureCardPDF(ctx, petID)
	if err != nil {
		return nil, err
	}
	if job.Status == "ready" && job.ObjectKey != "" && h.Objects != nil {
		url, err := h.presignPDF(ctx, job.ObjectKey, petID)
		if err != nil {
			return nil, err
		}
		if wantsJSON(req.Params.Accept) {
			return gen.GetPetCardPdf200JSONResponse{Url: url, ExpiresAt: h.now().Add(presignTTL)}, nil
		}
		return gen.GetPetCardPdf302Response{Headers: gen.GetPetCardPdf302ResponseHeaders{Location: &url}}, nil
	}
	return gen.GetPetCardPdf202JSONResponse(toDocumentJob(job, "", nil)), nil
}
