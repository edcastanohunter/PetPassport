package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Solicitud de vínculo por la clínica y alertas del perfil (PP-F2-DOM-013,
// PRD-020). La clínica pide, el tutor aprueba o rechaza; mientras está
// pendiente la clínica no ve la ficha. Caduca a los 14 días.

// CareLinkRequestTTL es la vigencia de una solicitud (PRD-020).
const CareLinkRequestTTL = 14 * 24 * time.Hour

// Límites de entrada (DOM-013).
const (
	MaxRequestMessageRunes = 300
	MaxAlertTextRunes      = 200
	MaxAlerts              = 20
)

// CareLinkRequestStatus es el estado de la solicitud.
type CareLinkRequestStatus string

// Estados de CareLinkRequest.
const (
	RequestPending  CareLinkRequestStatus = "pending"
	RequestApproved CareLinkRequestStatus = "approved"
	RequestDeclined CareLinkRequestStatus = "declined"
	RequestExpired  CareLinkRequestStatus = "expired"
)

// CareLinkRequest es la entidad interna del agregado Pet.
type CareLinkRequest struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	RequestedBy uuid.UUID
	Message     string
	ExpiresAt   time.Time
	Status      CareLinkRequestStatus
}

// Tipos de alerta del perfil.
const (
	AlertAllergy          = "allergy"
	AlertChronicCondition = "chronic_condition"
	AlertBehavior         = "behavior"
	AlertOther            = "other"
)

var alertKinds = map[string]bool{AlertAllergy: true, AlertChronicCondition: true, AlertBehavior: true, AlertOther: true}

// ---------------------------------------------------------------- errores

var (
	// ErrCareLinkAlreadyActive: la organización ya tiene vínculo activo.
	ErrCareLinkAlreadyActive = kernel.NewDomainError("CARE_LINK_ALREADY_ACTIVE", "La clínica ya tiene un vínculo activo con la mascota.")
	// ErrRequestPending: ya hay una solicitud pendiente de esa organización.
	ErrRequestPending = kernel.NewDomainError("REQUEST_PENDING", "Ya hay una solicitud pendiente de esta clínica para la mascota.")
	// ErrRequestNotFound: la solicitud no existe en la mascota.
	ErrRequestNotFound = kernel.NewDomainError("CARE_LINK_REQUEST_NOT_FOUND", "La solicitud no existe.")
	// ErrRequestNotPending: ya fue resuelta o caducó.
	ErrRequestNotPending = kernel.NewDomainError("CARE_LINK_REQUEST_NOT_PENDING", "La solicitud ya no está pendiente.")
)

// ---------------------------------------------------------------- eventos

// CareLinkRequested: la clínica pide el vínculo. El mensaje es texto libre del
// personal clínico, sin datos de contacto del tutor.
type CareLinkRequested struct {
	PetID          uuid.UUID `json:"pet_id"`
	RequestID      uuid.UUID `json:"request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	RequestedBy    uuid.UUID `json:"requested_by"`
	Message        string    `json:"message,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// EventName implementa eventstore.Event.
func (CareLinkRequested) EventName() string { return "CareLinkRequested" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkRequested) SchemaVersion() int { return 1 }

// CareLinkRequestApproved: el tutor aprueba (la concesión viaja en CareLinkGranted).
type CareLinkRequestApproved struct {
	PetID          uuid.UUID `json:"pet_id"`
	RequestID      uuid.UUID `json:"request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	ApprovedBy     uuid.UUID `json:"approved_by"`
}

// EventName implementa eventstore.Event.
func (CareLinkRequestApproved) EventName() string { return "CareLinkRequestApproved" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkRequestApproved) SchemaVersion() int { return 1 }

// CareLinkRequestDeclined: el tutor rechaza.
type CareLinkRequestDeclined struct {
	PetID          uuid.UUID `json:"pet_id"`
	RequestID      uuid.UUID `json:"request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	DeclinedBy     uuid.UUID `json:"declined_by"`
	Reason         string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (CareLinkRequestDeclined) EventName() string { return "CareLinkRequestDeclined" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkRequestDeclined) SchemaVersion() int { return 1 }

// CareLinkRequestExpired: venció sin respuesta (14 días).
type CareLinkRequestExpired struct {
	PetID          uuid.UUID `json:"pet_id"`
	RequestID      uuid.UUID `json:"request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

// EventName implementa eventstore.Event.
func (CareLinkRequestExpired) EventName() string { return "CareLinkRequestExpired" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkRequestExpired) SchemaVersion() int { return 1 }

// PetAlertsUpdated reemplaza las alertas del perfil.
type PetAlertsUpdated struct {
	PetID     uuid.UUID  `json:"pet_id"`
	Alerts    []PetAlert `json:"alerts"`
	UpdatedBy uuid.UUID  `json:"updated_by"`
}

// EventName implementa eventstore.Event.
func (PetAlertsUpdated) EventName() string { return "PetAlertsUpdated" }

// SchemaVersion implementa eventstore.Event.
func (PetAlertsUpdated) SchemaVersion() int { return 1 }

// -------------------------------------------------------------- comandos

// RequestCareLink: la clínica pide el vínculo (P1 —organización clínica
// activa— se valida en app con OrganizationLookup).
type RequestCareLink struct {
	PetID          uuid.UUID
	RequestID      uuid.UUID
	OrganizationID uuid.UUID
	RequestedBy    uuid.UUID
	Message        string
	Now            time.Time
}

// CommandName implementa bus.Command.
func (RequestCareLink) CommandName() string { return "petregistry.RequestCareLink" }

// ApproveCareLinkRequest: el tutor aprueba (equivale a conceder el vínculo).
type ApproveCareLinkRequest struct {
	PetID     uuid.UUID
	RequestID uuid.UUID
	ActorID   uuid.UUID
	Now       time.Time
}

// CommandName implementa bus.Command.
func (ApproveCareLinkRequest) CommandName() string { return "petregistry.ApproveCareLinkRequest" }

// DeclineCareLinkRequest: el tutor rechaza.
type DeclineCareLinkRequest struct {
	PetID     uuid.UUID
	RequestID uuid.UUID
	ActorID   uuid.UUID
	Reason    string
	Now       time.Time
}

// CommandName implementa bus.Command.
func (DeclineCareLinkRequest) CommandName() string { return "petregistry.DeclineCareLinkRequest" }

// ExpireCareLinkRequest lo lanza el scheduler de comandos a los 14 días.
// Idempotente: si ya se resolvió, o aún no vence, no emite nada.
type ExpireCareLinkRequest struct {
	PetID     uuid.UUID `json:"pet_id"`
	RequestID uuid.UUID `json:"request_id"`
	Now       time.Time `json:"-"` // el handler usa el reloj de ejecución
}

// CommandName implementa bus.Command.
func (ExpireCareLinkRequest) CommandName() string { return "petregistry.ExpireCareLinkRequest" }

// UpdatePetAlerts reemplaza las alertas del perfil (tutor o clínica vinculada).
type UpdatePetAlerts struct {
	PetID   uuid.UUID
	ActorID uuid.UUID
	Alerts  []PetAlert
}

// CommandName implementa bus.Command.
func (UpdatePetAlerts) CommandName() string { return "petregistry.UpdatePetAlerts" }

// --------------------------------------------------------------- handlers

func (p *Pet) pendingRequestFor(org uuid.UUID) bool {
	for _, r := range p.requests {
		if r.OrgID == org && r.Status == RequestPending {
			return true
		}
	}
	return false
}

func (p *Pet) handleRequestCareLink(c RequestCareLink) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	ve := &kernel.ValidationError{}
	msg := strings.TrimSpace(c.Message)
	if len([]rune(msg)) > MaxRequestMessageRunes {
		ve.Add("message", "TOO_LONG")
	}
	if c.RequestID == uuid.Nil {
		ve.Add("request_id", "INVALID")
	}
	if c.OrganizationID == uuid.Nil {
		ve.Add("organization_id", "INVALID")
	}
	if len(ve.Fields) > 0 {
		return nil, ve
	}
	if link, ok := p.careLinks[c.OrganizationID]; ok && link.Status == LinkActive { // P2
		return nil, ErrCareLinkAlreadyActive
	}
	if p.pendingRequestFor(c.OrganizationID) { // P2
		return nil, ErrRequestPending
	}
	p.record(CareLinkRequested{
		PetID: p.id, RequestID: c.RequestID, OrganizationID: c.OrganizationID,
		RequestedBy: c.RequestedBy, Message: msg, ExpiresAt: c.Now.Add(CareLinkRequestTTL),
	})
	return p.uncommitted, nil
}

// openRequest devuelve la solicitud pendiente y vigente, o el error que corresponde.
func (p *Pet) openRequest(id uuid.UUID, now time.Time) (CareLinkRequest, error) {
	r, ok := p.requests[id]
	if !ok {
		return CareLinkRequest{}, ErrRequestNotFound
	}
	if r.Status != RequestPending || !now.Before(r.ExpiresAt) {
		return CareLinkRequest{}, ErrRequestNotPending
	}
	return r, nil
}

func (p *Pet) handleApproveCareLinkRequest(c ApproveCareLinkRequest) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	r, err := p.openRequest(c.RequestID, c.Now)
	if err != nil {
		return nil, err
	}
	if p.activeLinks >= MaxActiveCareLinks {
		return nil, ErrCareLinkLimit
	}
	p.record(CareLinkRequestApproved{PetID: p.id, RequestID: r.ID, OrganizationID: r.OrgID, ApprovedBy: c.ActorID})
	by := r.RequestedBy
	p.record(CareLinkGranted{PetID: p.id, OrganizationID: r.OrgID, GrantedBy: c.ActorID, RequestedBy: &by})
	return p.uncommitted, nil
}

func (p *Pet) handleDeclineCareLinkRequest(c DeclineCareLinkRequest) ([]any, error) {
	r, err := p.openRequest(c.RequestID, c.Now)
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(c.Reason)
	if len([]rune(reason)) > MaxRequestMessageRunes {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reason", Code: "TOO_LONG"}}}
	}
	p.record(CareLinkRequestDeclined{PetID: p.id, RequestID: r.ID, OrganizationID: r.OrgID, DeclinedBy: c.ActorID, Reason: reason})
	return p.uncommitted, nil
}

func (p *Pet) handleExpireCareLinkRequest(c ExpireCareLinkRequest) ([]any, error) {
	r, ok := p.requests[c.RequestID]
	if !ok || r.Status != RequestPending || c.Now.Before(r.ExpiresAt) {
		return nil, nil // ya resuelta o aún vigente: idempotente
	}
	p.record(CareLinkRequestExpired{PetID: p.id, RequestID: r.ID, OrganizationID: r.OrgID})
	return p.uncommitted, nil
}

func (p *Pet) handleUpdatePetAlerts(c UpdatePetAlerts) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	ve := &kernel.ValidationError{}
	if len(c.Alerts) > MaxAlerts {
		ve.Add("alerts", "TOO_MANY")
	}
	clean := make([]PetAlert, 0, len(c.Alerts))
	for _, a := range c.Alerts {
		text := strings.TrimSpace(a.Text)
		if !alertKinds[a.Kind] {
			ve.Add("alerts.kind", "INVALID")
		}
		if text == "" || len([]rune(text)) > MaxAlertTextRunes {
			ve.Add("alerts.text", "INVALID")
		}
		clean = append(clean, PetAlert{Kind: a.Kind, Text: text})
	}
	if len(ve.Fields) > 0 {
		return nil, ve
	}
	if alertsEqual(p.alerts, clean) {
		return nil, ErrNoChanges
	}
	p.record(PetAlertsUpdated{PetID: p.id, Alerts: clean, UpdatedBy: c.ActorID})
	return p.uncommitted, nil
}

func alertsEqual(a, b []PetAlert) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
