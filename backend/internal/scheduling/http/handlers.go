// Package http implementa las operaciones de Scheduling del contrato OpenAPI
// (PP-F2-API-007/008) sobre el strict server generado. Cada handler:
// principal → vista de autorización (cita u organización) → authz.Can con la
// x-pp-action del contrato (404 si la cita no es visible, 403 si es visible
// sin permiso) → comando por el bus (If-Match o Idempotency-Key) → respuesta
// con ETag.
package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Service es la fachada de app.Service que usan los handlers.
type Service interface {
	Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error)
	Get(ctx context.Context, id uuid.UUID) (app.AppointmentView, error)
	List(ctx context.Context, f app.ListFilter) ([]app.AppointmentView, string, error)
	Agendas(ctx context.Context, orgID uuid.UUID) ([]*domain.ProfessionalAgenda, error)
	Availability(ctx context.Context, orgID uuid.UUID, professionalID *uuid.UUID,
		typ domain.AppointmentType, from, to time.Time, guardianView bool) ([]app.ProfessionalSlot, error)
}

// AgendaQueries lee una agenda y resuelve nombres de profesionales.
type AgendaQueries interface {
	Agenda(ctx context.Context, orgID, professionalID uuid.UUID) (*domain.ProfessionalAgenda, error)
	ProfessionalNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// Deps son las dependencias de los handlers.
type Deps struct {
	Service    Service
	Agendas    AgendaQueries
	Pets       app.PetAccess
	Membership app.Membership
	Idem       *idempotency.Store
	Logger     *slog.Logger
}

// SchedulingHandlers implementa las operaciones de Scheduling del contrato.
type SchedulingHandlers struct{ Deps }

// NewSchedulingHandlers construye los handlers.
func NewSchedulingHandlers(d Deps) *SchedulingHandlers {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &SchedulingHandlers{Deps: d}
}

func ptr[T any](v T) *T { return &v }

func (h *SchedulingHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "sin principal en el contexto"}
	}
	return p, nil
}

// meta construye el CommandContext del request.
func (h *SchedulingHandlers) meta(ctx context.Context, p auth.Principal, idemKey string, expected int64) context.Context {
	info := reqctx.From(ctx)
	return app.WithCommandContext(ctx, app.CommandContext{
		Principal:     app.PrincipalInfo{UserID: p.UserID, SuperAdmin: p.HasRole("super_admin")},
		CorrelationID: info.CorrelationID, CausationID: info.RequestID,
		IdempotencyKey: idemKey, ExpectedVersion: expected,
	})
}

// ------------------------------------------------------- autorización

// isClinicMember: el principal pertenece a una clínica (cualquier estado);
// basta para distinguir 403 de 404 (no revelar citas ajenas, ARQ-010).
func isClinicMember(p auth.Principal, orgID uuid.UUID) bool {
	for _, m := range p.Memberships {
		if m.OrganizationID == orgID && m.OrgType == "clinic" {
			return true
		}
	}
	return false
}

func apptResource(v app.AppointmentView) authz.AppointmentResource {
	return authz.AppointmentResource{ID: v.ID, OrganizationID: v.OrganizationID, GuardianID: v.GuardianID}
}

// loadAppt carga la cita y evalúa la acción: no visible → 404; visible sin
// permiso → 403. orgID != nil exige que la cita pertenezca a esa organización.
func (h *SchedulingHandlers) loadAppt(ctx context.Context, p auth.Principal, id uuid.UUID, orgID *uuid.UUID, action authz.Action) (app.AppointmentView, error) {
	v, err := h.Service.Get(ctx, id)
	if err != nil {
		return v, err
	}
	if orgID != nil && v.OrganizationID != *orgID {
		return v, &kernel.NotFoundError{Resource: "appointment"}
	}
	d := authz.Can(p, action, apptResource(v))
	if d.Allow {
		return v, nil
	}
	if p.UserID == v.GuardianID || isClinicMember(p, v.OrganizationID) {
		return v, &kernel.ForbiddenError{Reason: d.Reason}
	}
	return v, &kernel.NotFoundError{Resource: "appointment"}
}

// authorizeOrg evalúa una acción sobre una organización (403 si se deniega).
func authorizeOrg(p auth.Principal, action authz.Action, orgID uuid.UUID, linkedGuardian bool) error {
	d := authz.Can(p, action, authz.AppointmentResource{ID: orgID, OrganizationID: orgID, LinkedGuardian: linkedGuardian})
	if d.Allow {
		return nil
	}
	return &kernel.ForbiddenError{Reason: d.Reason}
}

// ------------------------------------------------------------- DTO

func toAPI(v app.AppointmentView) gen.Appointment {
	out := gen.Appointment{
		Id: v.ID, Version: v.Version, PetId: v.PetID, OrganizationId: v.OrganizationID,
		Type: gen.AppointmentType(v.Type), Origin: gen.AppointmentOrigin(v.Origin), Status: gen.AppointmentStatus(v.Status),
		Preferences: []gen.SlotPreference{}, History: []gen.AppointmentTransition{},
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
		ProposalExpiresAt: v.ProposalExpiresAt,
	}
	out.PetName, out.PetSpecies = nonEmpty(v.PetName), nonEmpty(v.PetSpecies)
	out.OrganizationName, out.ProfessionalName = nonEmpty(v.OrganizationName), nonEmpty(v.ProfessionalName)
	out.Reason, out.TimeZone = nonEmpty(v.Reason), nonEmpty(v.TZ)
	if v.ProfessionalID != nil {
		out.ProfessionalId = ptr(*v.ProfessionalID)
	}
	if v.EncounterID != nil {
		out.EncounterId = ptr(*v.EncounterID)
	}
	if v.Slot != nil {
		out.Start, out.End = ptr(v.Slot.Start), ptr(v.Slot.End)
	}
	if v.PolicyHours > 0 {
		out.CancellationPolicyHours = ptr(v.PolicyHours)
	}
	if v.Cancellation != nil {
		out.Cancellation = &gen.AppointmentCancellation{
			By: gen.AppointmentCancellationBy(v.Cancellation.By), Late: v.Cancellation.Late, Reason: nonEmpty(v.Cancellation.Reason),
		}
	}
	for _, pr := range v.Preferences {
		sp := gen.SlotPreference{Start: pr.Start, End: pr.End}
		if pr.DateFrom != nil {
			sp.DateFrom = ptr(string(*pr.DateFrom))
		}
		if pr.DateTo != nil {
			sp.DateTo = ptr(string(*pr.DateTo))
		}
		out.Preferences = append(out.Preferences, sp)
	}
	for _, t := range v.History {
		out.History = append(out.History, gen.AppointmentTransition{
			Status: gen.AppointmentStatus(t.Status), At: t.At, By: nonEmpty(t.By), Note: nonEmpty(t.Note),
		})
	}
	return out
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fromPreferences(in *[]gen.SlotPreference) ([]domain.Preference, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]domain.Preference, 0, len(*in))
	for _, sp := range *in {
		pr := domain.Preference{Start: sp.Start, End: sp.End}
		for _, d := range []struct {
			src *string
			dst **kernel.CivilDate
		}{{sp.DateFrom, &pr.DateFrom}, {sp.DateTo, &pr.DateTo}} {
			if d.src == nil {
				continue
			}
			cd, err := kernel.NewCivilDate(*d.src)
			if err != nil {
				ve := &kernel.ValidationError{}
				ve.Add("preferences", "INVALID")
				return nil, ve
			}
			*d.dst = &cd
		}
		out = append(out, pr)
	}
	return out, nil
}

func reasonOf(r *string) string {
	if r == nil {
		return ""
	}
	return *r
}

func required(field string) error {
	ve := &kernel.ValidationError{}
	ve.Add(field, "REQUIRED")
	return ve
}

// view relee la cita tras el comando (la proyección es síncrona, misma tx).
func (h *SchedulingHandlers) view(ctx context.Context, id uuid.UUID) (gen.Appointment, int64, error) {
	v, err := h.Service.Get(ctx, id)
	if err != nil {
		return gen.Appointment{}, 0, err
	}
	return toAPI(v), v.Version, nil
}

func okHeaders(version int64) gen.AppointmentOKResponseHeaders {
	return gen.AppointmentOKResponseHeaders{ETag: &version}
}

// ------------------------------------------------------- tutor

// RequestAppointment crea la solicitud del tutor (PRD-016).
func (h *SchedulingHandlers) RequestAppointment(ctx context.Context, req gen.RequestAppointmentRequestObject) (gen.RequestAppointmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, required("body")
	}
	pet, err := h.Pets.Pet(ctx, b.PetId)
	if err != nil {
		return nil, err
	}
	// Mascota ajena → 404 (no se revela su existencia).
	if d := authz.Can(p, authz.AppointmentRequest, authz.AppointmentResource{
		ID: uuid.New(), OrganizationID: b.OrganizationId, GuardianID: pet.GuardianID,
	}); !d.Allow {
		return nil, &kernel.NotFoundError{Resource: "pet"}
	}
	prefs, err := fromPreferences(b.Preferences)
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
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	cmd := app.RequestAppointment{
		AppointmentID: id, PetID: b.PetId, OrganizationID: b.OrganizationId,
		Type: string(b.Type), Reason: reasonOf(b.Reason), Preferences: prefs,
	}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	body, version, err := h.view(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.RequestAppointment201JSONResponse{AppointmentCreatedJSONResponse: gen.AppointmentCreatedJSONResponse{
		Body: body,
		Headers: gen.AppointmentCreatedResponseHeaders{
			ETag: &version, Location: ptr("/api/v1/appointments/" + id.String()),
		},
	}}, nil
}

// ListMyAppointments lista las citas de las mascotas del tutor.
func (h *SchedulingHandlers) ListMyAppointments(ctx context.Context, req gen.ListMyAppointmentsRequestObject) (gen.ListMyAppointmentsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AppointmentRead, authz.AppointmentResource{GuardianID: p.UserID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	f := app.ListFilter{GuardianID: ptr(p.UserID), PetID: req.Params.PetId, Cursor: derefStr(req.Params.Cursor), Limit: derefInt(req.Params.Limit)}
	if req.Params.Status != nil {
		f.Statuses = []string{string(*req.Params.Status)}
	}
	items, next, err := h.Service.List(ctx, f)
	if err != nil {
		return nil, err
	}
	return gen.ListMyAppointments200JSONResponse(page(items, next)), nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func page(items []app.AppointmentView, next string) gen.AppointmentPage {
	out := gen.AppointmentPage{Data: make([]gen.Appointment, 0, len(items))}
	for _, v := range items {
		out.Data = append(out.Data, toAPI(v))
	}
	if next != "" {
		out.NextCursor = &next
	}
	return out
}

// GetAppointment devuelve el detalle (tutor o miembro clínico).
func (h *SchedulingHandlers) GetAppointment(ctx context.Context, req gen.GetAppointmentRequestObject) (gen.GetAppointmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.loadAppt(ctx, p, req.AppointmentId, nil, authz.AppointmentRead)
	if err != nil {
		return nil, err
	}
	return gen.GetAppointment200JSONResponse{AppointmentOKJSONResponse: gen.AppointmentOKJSONResponse{Body: toAPI(v), Headers: okHeaders(v.Version)}}, nil
}

// AcceptAppointmentProposal acepta la alternativa propuesta (solo el tutor).
func (h *SchedulingHandlers) AcceptAppointmentProposal(ctx context.Context, req gen.AcceptAppointmentProposalRequestObject) (gen.AcceptAppointmentProposalResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.loadAppt(ctx, p, req.AppointmentId, nil, authz.AppointmentRequest); err != nil {
		return nil, err
	}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", 0), app.AcceptProposal{AppointmentID: req.AppointmentId}); err != nil {
		return nil, err
	}
	body, version, err := h.view(ctx, req.AppointmentId)
	if err != nil {
		return nil, err
	}
	return gen.AcceptAppointmentProposal200JSONResponse{AppointmentOKJSONResponse: gen.AppointmentOKJSONResponse{Body: body, Headers: okHeaders(version)}}, nil
}

// CancelAppointment cancela la cita (tutor).
func (h *SchedulingHandlers) CancelAppointment(ctx context.Context, req gen.CancelAppointmentRequestObject) (gen.CancelAppointmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.loadAppt(ctx, p, req.AppointmentId, nil, authz.AppointmentCancel); err != nil {
		return nil, err
	}
	cmd := app.CancelAppointment{AppointmentID: req.AppointmentId}
	if req.Body != nil {
		cmd.Reason = reasonOf(req.Body.Reason)
	}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", 0), cmd); err != nil {
		return nil, err
	}
	body, version, err := h.view(ctx, req.AppointmentId)
	if err != nil {
		return nil, err
	}
	return gen.CancelAppointment200JSONResponse{AppointmentOKJSONResponse: gen.AppointmentOKJSONResponse{Body: body, Headers: okHeaders(version)}}, nil
}

// ------------------------------------------------------- clínica

// CreateOrgAppointment crea una cita confirmada desde la clínica (PRD-017).
func (h *SchedulingHandlers) CreateOrgAppointment(ctx context.Context, req gen.CreateOrgAppointmentRequestObject) (gen.CreateOrgAppointmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := req.OrgId
	if err := authorizeOrg(p, authz.AppointmentManage, orgID, false); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, required("body")
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
	cmd := app.CreateAppointment{
		AppointmentID: id, PetID: b.PetId, OrganizationID: orgID, ProfessionalID: b.ProfessionalId,
		Type: string(b.Type), Reason: reasonOf(b.Reason), Start: b.Start, End: b.End,
	}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, key.String(), 0), cmd); err != nil {
		_ = h.Idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	body, version, err := h.view(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	if err := h.Idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.CreateOrgAppointment201JSONResponse{AppointmentCreatedJSONResponse: gen.AppointmentCreatedJSONResponse{
		Body: body,
		Headers: gen.AppointmentCreatedResponseHeaders{
			ETag: &version, Location: ptr("/api/v1/appointments/" + id.String()),
		},
	}}, nil
}

// ListOrgAppointments lista la bandeja (sin from/to) o la agenda (con ellos).
func (h *SchedulingHandlers) ListOrgAppointments(ctx context.Context, req gen.ListOrgAppointmentsRequestObject) (gen.ListOrgAppointmentsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := req.OrgId
	if err := authorizeOrg(p, authz.AppointmentManage, orgID, false); err != nil {
		return nil, err
	}
	f := app.ListFilter{
		OrganizationID: &orgID, ProfessionalID: req.Params.ProfessionalId, PetID: req.Params.PetId,
		From: req.Params.From, To: req.Params.To, Cursor: derefStr(req.Params.Cursor), Limit: derefInt(req.Params.Limit),
	}
	if req.Params.Status != nil {
		f.Statuses = []string{string(*req.Params.Status)}
	}
	items, next, err := h.Service.List(ctx, f)
	if err != nil {
		return nil, err
	}
	return gen.ListOrgAppointments200JSONResponse(page(items, next)), nil
}

// orgCommand es el patrón común de las acciones de la clínica sobre una cita:
// cargar + autorizar (misma organización), despachar y devolver la cita.
func (h *SchedulingHandlers) orgCommand(ctx context.Context, orgID, apptID uuid.UUID, action authz.Action, expected int64,
	cmd func(v app.AppointmentView) (bus.Command, error)) (gen.Appointment, int64, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return gen.Appointment{}, 0, err
	}
	v, err := h.loadAppt(ctx, p, apptID, &orgID, action)
	if err != nil {
		return gen.Appointment{}, 0, err
	}
	c, err := cmd(v)
	if err != nil {
		return gen.Appointment{}, 0, err
	}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", expected), c); err != nil {
		return gen.Appointment{}, 0, err
	}
	return h.view(ctx, apptID)
}

func ok(body gen.Appointment, version int64) gen.AppointmentOKJSONResponse {
	return gen.AppointmentOKJSONResponse{Body: body, Headers: okHeaders(version)}
}

// ConfirmAppointment confirma una solicitud con profesional y franja.
func (h *SchedulingHandlers) ConfirmAppointment(ctx context.Context, req gen.ConfirmAppointmentRequestObject) (gen.ConfirmAppointmentResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		b := req.Body
		if b == nil || b.ProfessionalId == nil {
			return nil, required("professional_id")
		}
		return app.ConfirmAppointment{
			AppointmentID: req.AppointmentId, OrganizationID: req.OrgId, ProfessionalID: *b.ProfessionalId, Start: b.Start, End: b.End,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.ConfirmAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// ProposeAppointmentAlternative propone otra franja (vigencia 48 h).
func (h *SchedulingHandlers) ProposeAppointmentAlternative(ctx context.Context, req gen.ProposeAppointmentAlternativeRequestObject) (gen.ProposeAppointmentAlternativeResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		b := req.Body
		if b == nil || b.ProfessionalId == nil {
			return nil, required("professional_id")
		}
		return app.ProposeAlternative{
			AppointmentID: req.AppointmentId, OrganizationID: req.OrgId, ProfessionalID: *b.ProfessionalId, Start: b.Start, End: b.End,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.ProposeAppointmentAlternative200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// RejectAppointment rechaza una solicitud o propuesta.
func (h *SchedulingHandlers) RejectAppointment(ctx context.Context, req gen.RejectAppointmentRequestObject) (gen.RejectAppointmentResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		r := ""
		if req.Body != nil {
			r = reasonOf(req.Body.Reason)
		}
		return app.RejectRequest{AppointmentID: req.AppointmentId, OrganizationID: req.OrgId, Reason: r}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.RejectAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// RescheduleAppointment mueve la cita confirmada (If-Match).
func (h *SchedulingHandlers) RescheduleAppointment(ctx context.Context, req gen.RescheduleAppointmentRequestObject) (gen.RescheduleAppointmentResponseObject, error) {
	if req.Body == nil {
		return nil, required("body")
	}
	b := req.Body
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, req.Params.IfMatch, func(app.AppointmentView) (bus.Command, error) {
		return app.RescheduleAppointment{
			AppointmentID: req.AppointmentId, OrganizationID: req.OrgId, ProfessionalID: b.ProfessionalId, Start: b.Start, End: b.End,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.RescheduleAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// CancelOrgAppointment cancela la cita desde la clínica.
func (h *SchedulingHandlers) CancelOrgAppointment(ctx context.Context, req gen.CancelOrgAppointmentRequestObject) (gen.CancelOrgAppointmentResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentCancel, 0, func(app.AppointmentView) (bus.Command, error) {
		r := ""
		if req.Body != nil {
			r = reasonOf(req.Body.Reason)
		}
		return app.CancelAppointment{AppointmentID: req.AppointmentId, OrganizationID: ptr(req.OrgId), Reason: r}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.CancelOrgAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// CheckInAppointment registra la llegada.
func (h *SchedulingHandlers) CheckInAppointment(ctx context.Context, req gen.CheckInAppointmentRequestObject) (gen.CheckInAppointmentResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		return app.CheckInAppointment{AppointmentID: req.AppointmentId, OrganizationID: req.OrgId}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.CheckInAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// MarkAppointmentNoShow marca la ausencia.
func (h *SchedulingHandlers) MarkAppointmentNoShow(ctx context.Context, req gen.MarkAppointmentNoShowRequestObject) (gen.MarkAppointmentNoShowResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		return app.MarkAppointmentNoShow{AppointmentID: req.AppointmentId, OrganizationID: req.OrgId}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.MarkAppointmentNoShow200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// CompleteAppointment completa la cita a mano.
func (h *SchedulingHandlers) CompleteAppointment(ctx context.Context, req gen.CompleteAppointmentRequestObject) (gen.CompleteAppointmentResponseObject, error) {
	body, v, err := h.orgCommand(ctx, req.OrgId, req.AppointmentId, authz.AppointmentManage, 0, func(app.AppointmentView) (bus.Command, error) {
		return app.CompleteAppointment{AppointmentID: req.AppointmentId, OrganizationID: req.OrgId}, nil
	})
	if err != nil {
		return nil, err
	}
	return gen.CompleteAppointment200JSONResponse{AppointmentOKJSONResponse: ok(body, v)}, nil
}

// ------------------------------------------------------- agendas

func toAgenda(a *domain.ProfessionalAgenda, name string) gen.Agenda {
	cfg := a.Config()
	tpl := make(map[string][]gen.AgendaTimeRange, len(cfg.WeeklyTemplate))
	for day, rs := range cfg.WeeklyTemplate {
		tpl[day] = toRanges(rs)
	}
	dur := make(map[string]int, len(cfg.SlotMinutes))
	for t, m := range cfg.SlotMinutes {
		dur[string(t)] = m
	}
	exc := make(map[string]gen.AgendaDayException, len(a.Exceptions()))
	for date, e := range a.Exceptions() {
		ex := gen.AgendaDayException{Closed: e.Closed}
		if len(e.Ranges) > 0 {
			ex.Ranges = ptr(toRanges(e.Ranges))
		}
		exc[date] = ex
	}
	return gen.Agenda{
		OrganizationId: a.Organization(), ProfessionalId: a.Professional(), ProfessionalName: nonEmpty(name),
		Defined: true, Active: a.Active(), Version: a.Version(), TimeZone: nonEmpty(cfg.TimeZone),
		WeeklyTemplate: &tpl, SlotMinutes: &dur, Exceptions: &exc,
		LeadTimeMinHours: ptr(cfg.LeadMinHours), LeadTimeMaxDays: ptr(cfg.LeadMaxDays),
		CancellationPolicyHours: ptr(cfg.CancelPolicyHrs),
	}
}

func toRanges(in []domain.TimeRange) []gen.AgendaTimeRange {
	out := make([]gen.AgendaTimeRange, 0, len(in))
	for _, r := range in {
		out = append(out, gen.AgendaTimeRange{Start: r.Start, End: r.End})
	}
	return out
}

func fromRanges(in []gen.AgendaTimeRange) []domain.TimeRange {
	out := make([]domain.TimeRange, 0, len(in))
	for _, r := range in {
		out = append(out, domain.TimeRange{Start: r.Start, End: r.End})
	}
	return out
}

// agendaView relee la agenda proyectada (misma tx que el comando).
func (h *SchedulingHandlers) agendaView(ctx context.Context, orgID, profID uuid.UUID) (gen.Agenda, error) {
	a, err := h.Agendas.Agenda(ctx, orgID, profID)
	if err != nil {
		return gen.Agenda{}, err
	}
	names, err := h.Agendas.ProfessionalNames(ctx, []uuid.UUID{profID})
	if err != nil {
		return gen.Agenda{}, err
	}
	return toAgenda(a, names[profID]), nil
}

// ListOrgAgendas lista las agendas e incluye los veterinarios sin agenda.
func (h *SchedulingHandlers) ListOrgAgendas(ctx context.Context, req gen.ListOrgAgendasRequestObject) (gen.ListOrgAgendasResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := req.OrgId
	if err := authorizeOrg(p, authz.AgendaRead, orgID, false); err != nil {
		return nil, err
	}
	agendas, err := h.Service.Agendas(ctx, orgID)
	if err != nil {
		return nil, err
	}
	vets, err := h.Membership.Veterinarians(ctx, orgID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(agendas)+len(vets))
	have := map[uuid.UUID]bool{}
	for _, a := range agendas {
		ids = append(ids, a.Professional())
		have[a.Professional()] = true
	}
	for _, v := range vets {
		if !have[v] {
			ids = append(ids, v)
		}
	}
	names := map[uuid.UUID]string{}
	if len(ids) > 0 {
		if names, err = h.Agendas.ProfessionalNames(ctx, ids); err != nil {
			return nil, err
		}
	}
	out := gen.AgendaList{Data: make([]gen.Agenda, 0, len(ids))}
	for _, a := range agendas {
		out.Data = append(out.Data, toAgenda(a, names[a.Professional()]))
	}
	for _, v := range vets {
		if !have[v] {
			out.Data = append(out.Data, gen.Agenda{
				OrganizationId: orgID, ProfessionalId: v, ProfessionalName: nonEmpty(names[v]), Defined: false,
			})
		}
	}
	sort.SliceStable(out.Data, func(i, j int) bool {
		return derefStr(out.Data[i].ProfessionalName) < derefStr(out.Data[j].ProfessionalName)
	})
	return gen.ListOrgAgendas200JSONResponse(out), nil
}

// PutOrgAgenda define o actualiza la agenda de un profesional.
func (h *SchedulingHandlers) PutOrgAgenda(ctx context.Context, req gen.PutOrgAgendaRequestObject) (gen.PutOrgAgendaResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID, profID := req.OrgId, req.ProfessionalId
	if err := authorizeOrg(p, authz.AgendaManage, orgID, false); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, required("body")
	}
	// Una agenda existente exige If-Match (control de concurrencia optimista).
	_, readErr := h.Agendas.Agenda(ctx, orgID, profID)
	var nf *kernel.NotFoundError
	exists := readErr == nil
	if readErr != nil && !errors.As(readErr, &nf) {
		return nil, readErr
	}
	var expected int64
	if exists {
		if req.Params.IfMatch == nil {
			return nil, required("If-Match")
		}
		expected = *req.Params.IfMatch
	}
	cfg := domain.AgendaConfig{
		WeeklyTemplate: map[string][]domain.TimeRange{}, SlotMinutes: map[domain.AppointmentType]int{},
		LeadMinHours: b.LeadTimeMinHours, LeadMaxDays: b.LeadTimeMaxDays, CancelPolicyHrs: b.CancellationPolicyHours,
	}
	if b.TimeZone != nil {
		cfg.TimeZone = *b.TimeZone
	}
	for day, rs := range b.WeeklyTemplate {
		cfg.WeeklyTemplate[day] = fromRanges(rs)
	}
	for t, m := range b.SlotMinutes {
		cfg.SlotMinutes[domain.AppointmentType(t)] = m
	}
	active := b.Active == nil || *b.Active
	cmd := app.SaveAgenda{OrganizationID: orgID, ProfessionalID: profID, Config: cfg, Active: active}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", expected), cmd); err != nil {
		return nil, err
	}
	return agendaOKWrap(h, ctx, orgID, profID, func(a gen.Agenda) gen.PutOrgAgendaResponseObject {
		return gen.PutOrgAgenda200JSONResponse{AgendaOKJSONResponse: agendaResp(a)}
	})
}

func agendaResp(a gen.Agenda) gen.AgendaOKJSONResponse {
	v := a.Version
	return gen.AgendaOKJSONResponse{Body: a, Headers: gen.AgendaOKResponseHeaders{ETag: &v}}
}

func agendaOKWrap[T any](h *SchedulingHandlers, ctx context.Context, orgID, profID uuid.UUID, wrap func(gen.Agenda) T) (T, error) {
	var zero T
	a, err := h.agendaView(ctx, orgID, profID)
	if err != nil {
		return zero, err
	}
	return wrap(a), nil
}

// AddAgendaException añade una excepción de día.
func (h *SchedulingHandlers) AddAgendaException(ctx context.Context, req gen.AddAgendaExceptionRequestObject) (gen.AddAgendaExceptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := authorizeOrg(p, authz.AgendaManage, req.OrgId, false); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, required("body")
	}
	ex := domain.DayException{Closed: b.Closed}
	if b.Ranges != nil {
		ex.Ranges = fromRanges(*b.Ranges)
	}
	cmd := app.AddAgendaException{OrganizationID: req.OrgId, ProfessionalID: req.ProfessionalId, Date: b.Date, Exception: ex}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", 0), cmd); err != nil {
		return nil, err
	}
	return agendaOKWrap(h, ctx, req.OrgId, req.ProfessionalId, func(a gen.Agenda) gen.AddAgendaExceptionResponseObject {
		return gen.AddAgendaException200JSONResponse{AgendaOKJSONResponse: agendaResp(a)}
	})
}

// RemoveAgendaException quita la excepción de un día.
func (h *SchedulingHandlers) RemoveAgendaException(ctx context.Context, req gen.RemoveAgendaExceptionRequestObject) (gen.RemoveAgendaExceptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := authorizeOrg(p, authz.AgendaManage, req.OrgId, false); err != nil {
		return nil, err
	}
	cmd := app.RemoveAgendaException{OrganizationID: req.OrgId, ProfessionalID: req.ProfessionalId, Date: req.Date}
	if _, err := h.Service.Dispatch(h.meta(ctx, p, "", 0), cmd); err != nil {
		return nil, err
	}
	return agendaOKWrap(h, ctx, req.OrgId, req.ProfessionalId, func(a gen.Agenda) gen.RemoveAgendaExceptionResponseObject {
		return gen.RemoveAgendaException200JSONResponse{AgendaOKJSONResponse: agendaResp(a)}
	})
}

// GetOrgAvailability devuelve las franjas libres publicadas.
func (h *SchedulingHandlers) GetOrgAvailability(ctx context.Context, req gen.GetOrgAvailabilityRequestObject) (gen.GetOrgAvailabilityResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := req.OrgId
	linked, err := h.Pets.GuardianLinked(ctx, p.UserID, orgID)
	if err != nil {
		return nil, err
	}
	if err := authorizeOrg(p, authz.AvailabilityRead, orgID, linked); err != nil {
		return nil, &kernel.NotFoundError{Resource: "organization"} // no revelar la agenda de clínicas ajenas
	}
	guardianView := !isClinicMember(p, orgID)
	slots, err := h.Service.Availability(ctx, orgID, req.Params.ProfessionalId,
		domain.AppointmentType(req.Params.Type), req.Params.From, req.Params.To, guardianView)
	if err != nil {
		return nil, err
	}
	out := gen.AvailabilityList{Data: make([]gen.AvailabilitySlot, 0, len(slots))}
	for _, s := range slots {
		out.Data = append(out.Data, gen.AvailabilitySlot{ProfessionalId: s.ProfessionalID, Start: s.Start, End: s.End})
	}
	return gen.GetOrgAvailability200JSONResponse(out), nil
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

func (r replayed) VisitRequestAppointmentResponse(w http.ResponseWriter) error { return r.write(w) }

func (r replayed) VisitCreateOrgAppointmentResponse(w http.ResponseWriter) error { return r.write(w) }

var (
	_ gen.RequestAppointmentResponseObject   = replayed{}
	_ gen.CreateOrgAppointmentResponseObject = replayed{}
)
