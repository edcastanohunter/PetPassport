package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// MaxActiveCareLinks es I8: máximo 20 vínculos activos por mascota.
const MaxActiveCareLinks = 20

// UndoWindow es el plazo para deshacer un fallecimiento (PRD-009).
const UndoWindow = 24 * time.Hour

// TransferTTL es la vigencia de una transferencia de tutela (PRD-010).
const TransferTTL = 7 * 24 * time.Hour

// StreamType es el tipo de stream del agregado (pet-<PetID>).
const StreamType = "Pet"

// CareLink es el vínculo con una organización dentro del agregado (entidad
// interna, no raíz; docs/phase-1/02 §9).
type CareLink struct {
	OrgID     uuid.UUID
	Status    CareLinkStatus
	GrantedBy uuid.UUID
}

// GuardianshipTransfer es la transferencia pendiente única del agregado.
// El hash del token vive en read_petregistry.pet_transfers, no aquí: el
// evento no lo persiste (DOM-005) y la aceptación se resuelve por consulta.
type GuardianshipTransfer struct {
	ToEmail   kernel.Email
	ExpiresAt time.Time
}

// Pet es la raíz de agregado (PP-F1-DOM-003). Rehidratable por replay:
// zero value + ApplyEvent por evento. Version es la versión del stream
// (número de eventos aplicados); los eventos no confirmados aún cuentan en
// la versión, así que el repositorio persiste con expected =
// Version - len(uncommitted).
type Pet struct {
	id           uuid.UUID
	version      int64
	guardianID   uuid.UUID
	name         PetName
	species      Species
	speciesOther string
	breed        *string
	sex          Sex
	neutered     Tristate
	birthDate    *BirthDate
	markings     *string
	microchip    *Microchip
	corrections  int
	photo        *PhotoRef
	status       PetStatus
	deceasedAt   *kernel.CivilDate
	careLinks    map[uuid.UUID]CareLink
	activeLinks  int
	pending      *GuardianshipTransfer
	uncommitted  []any
}

// ID devuelve el identificador del agregado.
func (p *Pet) ID() uuid.UUID { return p.id }

// Version devuelve la versión actual del stream.
func (p *Pet) Version() int64 { return p.version }

// GuardianID devuelve el tutor actual.
func (p *Pet) GuardianID() uuid.UUID { return p.guardianID }

// Name devuelve el nombre (emails de transferencia).
func (p *Pet) Name() string { return string(p.name) }

// Status devuelve el estado vital.
func (p *Pet) Status() PetStatus { return p.status }

// ActiveCareLinkOrgIDs devuelve las orgs con vínculo activo (authz, L).
func (p *Pet) ActiveCareLinkOrgIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p.careLinks))
	for id, l := range p.careLinks {
		if l.Status == LinkActive {
			out = append(out, id)
		}
	}
	return out
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (p *Pet) UncommittedEvents() []any { return p.uncommitted }

// MarkCommitted limpia los eventos no confirmados tras el Append.
func (p *Pet) MarkCommitted() { p.uncommitted = nil }

// IsDeceased informa si la mascota está fallecida (I3).
func (p *Pet) IsDeceased() bool { return p.status == PetDeceased }

// MicrochipValue devuelve el microchip actual ("" si no tiene).
func (p *Pet) MicrochipValue() string {
	if p.microchip == nil {
		return ""
	}
	return string(*p.microchip)
}

// ---------------------------------------------------------------------------
// Rehidratación
// ---------------------------------------------------------------------------

// ApplyEvent muta el estado al rehidratar (replay del event store y Given del
// harness). Evento desconocido → error (fail fast).
func (p *Pet) ApplyEvent(evt any) error {
	switch e := evt.(type) {
	case PetRegistered:
		p.id = e.PetID
		p.guardianID = e.GuardianID
		p.name = PetName(e.Name)
		p.species = e.Species
		p.speciesOther = e.SpeciesOther
		if e.Breed != "" {
			b := string(e.Breed)
			p.breed = &b
		}
		p.sex = e.Sex
		p.neutered = e.Neutered
		if e.BirthDate != nil {
			p.birthDate = &BirthDate{Date: kernel.CivilDate(e.BirthDate.Date), Approximate: e.BirthDate.Approximate}
		}
		if e.Markings != "" {
			m := string(e.Markings)
			p.markings = &m
		}
		p.status = PetActive
		p.careLinks = map[uuid.UUID]CareLink{}
	case PetProfileUpdated:
		p.applyChanges(e.Changes)
	case PetMicrochipAssigned:
		m := Microchip(e.Microchip)
		if e.Previous != "" {
			p.corrections++
		}
		p.microchip = &m
	case PetPhotoChanged:
		if e.Photo != nil {
			ref := *e.Photo
			p.photo = &ref
		} else {
			p.photo = nil
		}
	case PetMarkedDeceased:
		p.status = PetDeceased
		if e.DeceasedAt != "" {
			d := kernel.CivilDate(e.DeceasedAt)
			p.deceasedAt = &d
		} else {
			p.deceasedAt = nil
		}
	case PetDeceasedUndone:
		p.status = PetActive
		p.deceasedAt = nil
	case CareLinkGranted:
		if cur, ok := p.careLinks[e.OrganizationID]; !ok || cur.Status != LinkActive {
			p.activeLinks++
		}
		p.careLinks[e.OrganizationID] = CareLink{OrgID: e.OrganizationID, Status: LinkActive, GrantedBy: e.GrantedBy}
	case CareLinkRevoked:
		if cur, ok := p.careLinks[e.OrganizationID]; ok && cur.Status == LinkActive {
			p.activeLinks--
			cur.Status = LinkRevoked
			p.careLinks[e.OrganizationID] = cur
		}
	case GuardianshipTransferInitiated:
		if e.PII == nil {
			return &kernel.DomainError{Code: "UNKNOWN_EVENT", Message: "transferencia sin destinatario"}
		}
		p.pending = &GuardianshipTransfer{
			ToEmail:   e.PII.ToEmail,
			ExpiresAt: e.ExpiresAt,
		}
		// El hash del token no viaja en el evento: vive en read_petregistry.pet_transfers
		// (DAT-005); la app resuelve token → mascota por consulta antes de despachar
		// el comando (ARQ-003: consulta sobre read model, comando sobre un agregado).
	case GuardianshipTransferCancelled:
		p.pending = nil
	case PetGuardianshipTransferred:
		p.guardianID = e.ToGuardianID
		p.pending = nil
	default:
		return &kernel.DomainError{Code: "UNKNOWN_EVENT", Message: "evento desconocido para Pet"}
	}
	p.version++
	return nil
}

func (p *Pet) applyChanges(c Changes) {
	if c.Name != nil {
		p.name = PetName(*c.Name)
	}
	if c.Species != nil {
		p.species = Species(*c.Species)
	}
	if c.SpeciesOther != nil {
		p.speciesOther = *c.SpeciesOther
	}
	if c.Breed != nil {
		if *c.Breed == "" {
			p.breed = nil
		} else {
			b := *c.Breed
			p.breed = &b
		}
	}
	if c.Sex != nil {
		p.sex = Sex(*c.Sex)
	}
	if c.Neutered != nil {
		p.neutered = Tristate(*c.Neutered)
	}
	if c.BirthDate != nil {
		if c.BirthDate.Date == "" {
			p.birthDate = nil
		} else {
			p.birthDate = &BirthDate{Date: kernel.CivilDate(c.BirthDate.Date), Approximate: c.BirthDate.Approximate}
		}
	}
	if c.Markings != nil {
		if *c.Markings == "" {
			p.markings = nil
		} else {
			m := *c.Markings
			p.markings = &m
		}
	}
}

// record aplica el evento y lo deja como no confirmado.
func (p *Pet) record(evt any) {
	if err := p.ApplyEvent(evt); err != nil {
		panic("petregistry: aplicar evento recién emitido: " + err.Error())
	}
	p.uncommitted = append(p.uncommitted, evt)
}

// ---------------------------------------------------------------------------
// Comandos
// ---------------------------------------------------------------------------

// HandleCommand ejecuta un comando y devuelve los eventos a persistir.
func (p *Pet) HandleCommand(cmd any) ([]any, error) {
	switch c := cmd.(type) {
	case RegisterPet:
		return p.handleRegister(c)
	case UpdatePetProfile:
		return p.handleUpdateProfile(c)
	case AssignMicrochip:
		return p.handleAssignMicrochip(c)
	case ChangePetPhoto:
		return p.handlePhoto(c)
	case MarkPetDeceased:
		return p.handleDeceased(c)
	case UndoDeceased:
		return p.handleUndoDeceased(c)
	case GrantCareLink:
		return p.handleGrantCareLink(c)
	case RevokeCareLink:
		return p.handleRevokeCareLink(c)
	case InitiateGuardianshipTransfer:
		return p.handleInitiateTransfer(c)
	case CancelGuardianshipTransfer:
		return p.handleCancelTransfer(c)
	case AcceptGuardianshipTransfer:
		return p.handleAcceptTransfer(c)
	}
	return nil, &kernel.DomainError{Code: "UNKNOWN_COMMAND", Message: "comando desconocido para Pet"}
}

func (p *Pet) handleRegister(c RegisterPet) ([]any, error) {
	if p.version != 0 {
		return nil, kernel.NewDomainError("PET_ALREADY_REGISTERED", "La mascota ya está registrada.")
	}
	ve := &kernel.ValidationError{}
	name, err := NewPetName(c.Name)
	if err != nil {
		ve.Add("name", "INVALID")
	}
	if c.GuardianID == uuid.Nil {
		ve.Add("guardian_id", "INVALID")
	}
	species, err := NewSpecies(c.Species)
	if err != nil {
		ve.Add("species", "INVALID")
	} else if species.IsOther() {
		so := strings.TrimSpace(c.SpeciesOther)
		if so == "" || len([]rune(so)) > 40 {
			ve.Add("species_other", "INVALID")
		}
	} else if strings.TrimSpace(c.SpeciesOther) != "" {
		ve.Add("species_other", "NOT_ALLOWED")
	}
	var breed string
	if c.Breed != nil && strings.TrimSpace(*c.Breed) != "" {
		b, err := NewBreed(*c.Breed)
		if err != nil {
			ve.Add("breed", "INVALID")
		} else {
			breed = string(b)
		}
	}
	sex, err := NewSex(c.Sex)
	if err != nil {
		ve.Add("sex", "INVALID")
	}
	neutered, err := NewTristate(c.Neutered)
	if err != nil {
		ve.Add("neutered", "INVALID")
	}
	var birth *BirthDatePayload
	if c.BirthDate != nil && c.BirthDate.Date != "" {
		bd, err := NewBirthDate(c.BirthDate.Date, c.BirthDate.Approximate, kernel.CivilDateOf(c.Now))
		if err != nil {
			addFieldError(ve, err, "birth_date")
		} else {
			birth = &BirthDatePayload{Date: bd.Date.String(), Approximate: bd.Approximate}
		}
	}
	var markings string
	if c.Markings != nil {
		m, err := NewMarkings(*c.Markings)
		if err != nil {
			ve.Add("markings", "INVALID")
		} else {
			markings = string(m)
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	id := c.PetID
	if id == uuid.Nil {
		id = kernel.NewID()
	}
	evt := PetRegistered{
		PetID: id, GuardianID: c.GuardianID, Name: string(name), Species: species,
		SpeciesOther: strings.TrimSpace(c.SpeciesOther), Breed: breed, Sex: sex,
		Neutered: neutered, BirthDate: birth, Markings: markings,
	}
	p.record(evt)
	return p.uncommitted, nil
}

func (p *Pet) handleUpdateProfile(c UpdatePetProfile) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	ve := &kernel.ValidationError{}
	ch := Changes{}
	if c.Name != nil {
		name, err := NewPetName(*c.Name)
		if err != nil {
			ve.Add("name", "INVALID")
		} else if string(name) != string(p.name) {
			v := string(name)
			ch.Name = &v
		}
	}
	targetSpecies := p.species
	if c.Species != nil {
		s, err := NewSpecies(*c.Species)
		if err != nil {
			ve.Add("species", "INVALID")
		} else {
			targetSpecies = s
			if s != p.species {
				v := string(s)
				ch.Species = &v
			}
		}
	}
	so := p.speciesOther
	if c.SpeciesOther != nil {
		so = strings.TrimSpace(*c.SpeciesOther)
	}
	if targetSpecies.IsOther() {
		if so == "" || len([]rune(so)) > 40 {
			ve.Add("species_other", "INVALID")
		}
	} else if targetSpecies != p.species {
		// Cambio de "other" a especie cerrada: el texto libre se limpia.
		so = ""
	} else if so != "" {
		ve.Add("species_other", "NOT_ALLOWED")
	}
	if c.SpeciesOther != nil || ch.Species != nil {
		if so != p.speciesOther {
			ch.SpeciesOther = &so
		}
	}
	if c.Breed != nil {
		trimmed := strings.TrimSpace(*c.Breed)
		if trimmed != "" {
			b, err := NewBreed(trimmed)
			if err != nil {
				ve.Add("breed", "INVALID")
			} else {
				trimmed = string(b)
			}
		}
		cur := ""
		if p.breed != nil {
			cur = *p.breed
		}
		if trimmed != cur {
			ch.Breed = &trimmed
		}
	}
	if c.Sex != nil {
		s, err := NewSex(*c.Sex)
		if err != nil {
			ve.Add("sex", "INVALID")
		} else if s != p.sex {
			v := string(s)
			ch.Sex = &v
		}
	}
	if c.Neutered != nil {
		n, err := NewTristate(*c.Neutered)
		if err != nil {
			ve.Add("neutered", "INVALID")
		} else if n != p.neutered {
			v := string(n)
			ch.Neutered = &v
		}
	}
	if c.BirthDate != nil {
		if c.BirthDate.Date == "" {
			if p.birthDate != nil {
				ch.BirthDate = &BirthDatePayload{}
			}
		} else {
			bd, err := NewBirthDate(c.BirthDate.Date, c.BirthDate.Approximate, kernel.CivilDateOf(c.Now))
			if err != nil {
				addFieldError(ve, err, "birth_date")
			} else {
				if p.birthDate == nil || p.birthDate.Date != bd.Date || p.birthDate.Approximate != bd.Approximate {
					ch.BirthDate = &BirthDatePayload{Date: bd.Date.String(), Approximate: bd.Approximate}
				}
			}
		}
	}
	if c.Markings != nil {
		trimmed := strings.TrimSpace(*c.Markings)
		if trimmed != "" {
			m, err := NewMarkings(trimmed)
			if err != nil {
				ve.Add("markings", "INVALID")
			} else {
				trimmed = string(m)
			}
		}
		cur := ""
		if p.markings != nil {
			cur = *p.markings
		}
		if trimmed != cur {
			ch.Markings = &trimmed
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if ch == (Changes{}) {
		return nil, nil // sin cambios reales: no se emite evento (DOM-003)
	}
	p.record(PetProfileUpdated{PetID: p.id, Changes: ch})
	return p.uncommitted, nil
}

func (p *Pet) handleAssignMicrochip(c AssignMicrochip) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	m, err := NewMicrochip(c.Microchip)
	if err != nil {
		return nil, err
	}
	if p.microchip != nil && *p.microchip == m {
		return nil, nil // idempotente: ya lo tiene
	}
	previous := ""
	if p.microchip != nil {
		if p.corrections >= 1 {
			return nil, ErrMicrochipCorrectionLimit
		}
		previous = string(*p.microchip)
	}
	p.record(PetMicrochipAssigned{PetID: p.id, Microchip: string(m), Previous: previous})
	return p.uncommitted, nil
}

func (p *Pet) handlePhoto(c ChangePetPhoto) ([]any, error) {
	if c.Photo != nil {
		if _, err := NewPhotoRef(p.id, c.Photo.ObjectKey, c.Photo.ThumbKey, c.Photo.ContentType); err != nil {
			return nil, err
		}
		if p.photo != nil && *p.photo == *c.Photo {
			return nil, nil
		}
	} else if p.photo == nil {
		return nil, nil // quitar foto que no existe
	}
	evt := PetPhotoChanged{PetID: p.id, Photo: c.Photo}
	if p.photo != nil {
		prev := *p.photo
		evt.Previous = &prev
	}
	p.record(evt)
	return p.uncommitted, nil
}

func (p *Pet) handleDeceased(c MarkPetDeceased) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	var at string
	if c.DeceasedAt != nil && strings.TrimSpace(*c.DeceasedAt) != "" {
		d, err := kernel.NewCivilDate(strings.TrimSpace(*c.DeceasedAt))
		if err != nil {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "deceased_at", Code: "INVALID"}}}
		}
		today := kernel.CivilDateOf(c.Now)
		if d.After(today) || (p.birthDate != nil && d.Before(p.birthDate.Date)) {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "deceased_at", Code: "OUT_OF_RANGE"}}}
		}
		at = d.String()
	}
	p.record(PetMarkedDeceased{PetID: p.id, DeceasedAt: at})
	return p.uncommitted, nil
}

func (p *Pet) handleUndoDeceased(c UndoDeceased) ([]any, error) {
	if p.status != PetDeceased {
		return nil, ErrPetNotDeceased
	}
	if !c.WithinUndoWindow && !c.ActorIsSuperAdmin {
		return nil, ErrUndoWindowExpired
	}
	p.record(PetDeceasedUndone{PetID: p.id})
	return p.uncommitted, nil
}

func (p *Pet) handleGrantCareLink(c GrantCareLink) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	if link, ok := p.careLinks[c.OrganizationID]; ok && link.Status == LinkActive {
		return nil, nil // idempotente (DOM-003)
	}
	if p.activeLinks >= MaxActiveCareLinks {
		return nil, ErrCareLinkLimit
	}
	p.record(CareLinkGranted{PetID: p.id, OrganizationID: c.OrganizationID, GrantedBy: c.ActorID})
	return p.uncommitted, nil
}

func (p *Pet) handleRevokeCareLink(c RevokeCareLink) ([]any, error) {
	link, ok := p.careLinks[c.OrganizationID]
	if !ok || link.Status != LinkActive {
		return nil, ErrCareLinkNotActive
	}
	p.record(CareLinkRevoked{PetID: p.id, OrganizationID: c.OrganizationID, RevokedBy: c.ActorID, Reason: strings.TrimSpace(c.Reason)})
	return p.uncommitted, nil
}

func (p *Pet) handleInitiateTransfer(c InitiateGuardianshipTransfer) ([]any, error) {
	if p.status == PetDeceased {
		return nil, ErrPetDeceased
	}
	to, err := kernel.NewEmail(c.ToEmail)
	if err != nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "to_email", Code: "INVALID"}}}
	}
	current, err := kernel.NewEmail(c.CurrentGuardianEmail)
	if err != nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "to_email", Code: "INVALID"}}}
	}
	if to == current { // I4
		return nil, ErrSameGuardian
	}
	if p.pending != nil {
		return nil, ErrTransferAlreadyPending
	}
	if c.ExpiresAt.Before(c.Now) {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "expires_at", Code: "INVALID"}}}
	}
	p.record(GuardianshipTransferInitiated{PetID: p.id, FromGuardianID: p.guardianID, ExpiresAt: c.ExpiresAt, PII: &TransferPII{ToEmail: to}})
	return p.uncommitted, nil
}

func (p *Pet) handleCancelTransfer(_ CancelGuardianshipTransfer) ([]any, error) {
	if p.pending == nil {
		return nil, ErrTransferNotPending
	}
	p.record(GuardianshipTransferCancelled{PetID: p.id})
	return p.uncommitted, nil
}

func (p *Pet) handleAcceptTransfer(c AcceptGuardianshipTransfer) ([]any, error) {
	if p.pending == nil {
		return nil, ErrTransferNotPending
	}
	// El token ya se validó contra pet_transfers en app (404 si no coincide o
	// caducó, sin distinguir — API-004); aquí se valida vigencia y destinatario.
	if c.Now.After(p.pending.ExpiresAt) {
		return nil, ErrTransferTokenInvalid
	}
	actor, err := kernel.NewEmail(c.ActorEmail)
	if err != nil || actor != p.pending.ToEmail {
		return nil, ErrTransferEmailMismatch
	}
	if c.ActorUserID == uuid.Nil || c.ActorUserID == p.guardianID {
		return nil, ErrSameGuardian
	}
	p.record(PetGuardianshipTransferred{PetID: p.id, FromGuardianID: p.guardianID, ToGuardianID: c.ActorUserID})
	return p.uncommitted, nil
}

func addFieldError(ve *kernel.ValidationError, err error, field string) {
	var verr *kernel.ValidationError
	if errors.As(err, &verr) {
		ve.Fields = append(ve.Fields, verr.Fields...)
		return
	}
	ve.Add(field, "INVALID")
}
