package domain

import (
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// StreamType es el tipo de stream del agregado (enc-<EncounterID>, DOM-003).
const StreamType = "Encounter"

// StartWindowPast / StartWindowFuture acotan StartedAt al abrir (E2,
// PRD-001: editable ±24 h).
const (
	StartWindowPast   = 24 * time.Hour
	StartWindowFuture = time.Hour
)

// EditWindow es la ventana de edición de StartedAt en un borrador (PRD-001).
const EditWindow = 24 * time.Hour

// AbandonAfter es el plazo tras el que un borrador sin actividad se muestra
// como abandonado (PRD-005); lo calcula la consulta, no el agregado.
const AbandonAfter = 7 * 24 * time.Hour

// Encounter es la raíz de agregado (PP-F2-DOM-003). Rehidratable por replay:
// zero value + ApplyEvent por evento. Version es la versión del stream.
type Encounter struct {
	id             uuid.UUID
	version        int64
	petID          uuid.UUID
	organizationID uuid.UUID
	appointmentID  *uuid.UUID
	typ            EncounterType
	status         EncounterStatus
	startedAt      time.Time
	reason         Reason
	attendingVetID *uuid.UUID
	vitals         *Vitals
	subjective     *Narrative
	objective      *Objective
	diagnoses      []Diagnosis
	plan           Plan
	followUp       *FollowUpRecommendation
	attachments    map[uuid.UUID]Attachment
	signature      *Signature
	addenda        []Addendum
	lastActivityAt time.Time
	discardReason  string
	uncommitted    []any
}

// ID devuelve el identificador del encuentro.
func (e *Encounter) ID() uuid.UUID { return e.id }

// Version devuelve la versión del stream.
func (e *Encounter) Version() int64 { return e.version }

// PetID devuelve la mascota.
func (e *Encounter) PetID() uuid.UUID { return e.petID }

// OrganizationID devuelve la organización dueña del encuentro.
func (e *Encounter) OrganizationID() uuid.UUID { return e.organizationID }

// AppointmentID devuelve la cita enlazada, si existe.
func (e *Encounter) AppointmentID() *uuid.UUID { return e.appointmentID }

// Type devuelve el tipo de atención.
func (e *Encounter) Type() EncounterType { return e.typ }

// Status devuelve el estado.
func (e *Encounter) Status() EncounterStatus { return e.status }

// StartedAt devuelve la fecha/hora de inicio.
func (e *Encounter) StartedAt() time.Time { return e.startedAt }

// Reason devuelve el motivo.
func (e *Encounter) Reason() Reason { return e.reason }

// AttendingVetID devuelve el profesional responsable, si lo hay.
func (e *Encounter) AttendingVetID() *uuid.UUID { return e.attendingVetID }

// Vitals devuelve las constantes (nil si no se registraron).
func (e *Encounter) Vitals() *Vitals { return e.vitals }

// Subjective devuelve la anamnesis.
func (e *Encounter) Subjective() *Narrative { return e.subjective }

// Objective devuelve el examen físico.
func (e *Encounter) Objective() *Objective { return e.objective }

// Diagnoses devuelve una copia de los diagnósticos.
func (e *Encounter) Diagnoses() []Diagnosis { return append([]Diagnosis(nil), e.diagnoses...) }

// Plan devuelve el plan.
func (e *Encounter) Plan() Plan { return e.plan }

// FollowUp devuelve el control recomendado.
func (e *Encounter) FollowUp() *FollowUpRecommendation { return e.followUp }

// Attachments devuelve una copia de los adjuntos ordenada por ID estable.
func (e *Encounter) Attachments() []Attachment {
	out := make([]Attachment, 0, len(e.attachments))
	for _, a := range e.attachments {
		out = append(out, a)
	}
	// Orden determinista para proyección y PDF (map no lo garantiza).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID.String() < out[j-1].ID.String(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Attachment devuelve un adjunto por ID: primero del cuerpo y, si no, de una
// adenda. AttachmentCount cuenta solo el cuerpo (los de adenda viven en ella).
func (e *Encounter) Attachment(id uuid.UUID) (Attachment, bool) {
	if a, ok := e.attachments[id]; ok {
		return a, true
	}
	for _, ad := range e.addenda {
		for _, a := range ad.Attachments {
			if a.ID == id {
				return a, true
			}
		}
	}
	return Attachment{}, false
}

// AttachmentCount devuelve cuántos adjuntos hay en el cuerpo (las adendas no
// cuentan: sus adjuntos viven en la adenda).
func (e *Encounter) AttachmentCount() int { return len(e.attachments) }

// Signature devuelve la firma (nil en borrador).
func (e *Encounter) Signature() *Signature { return e.signature }

// Addenda devuelve una copia de las adendas.
func (e *Encounter) Addenda() []Addendum { return append([]Addendum(nil), e.addenda...) }

// LastActivityAt devuelve la última actividad registrada.
func (e *Encounter) LastActivityAt() time.Time { return e.lastActivityAt }

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (e *Encounter) UncommittedEvents() []any { return e.uncommitted }

// MarkCommitted limpia los eventos no confirmados tras el Append.
func (e *Encounter) MarkCommitted() { e.uncommitted = nil }

// record aplica el evento recién emitido y lo deja pendiente.
func (e *Encounter) record(evt any) {
	if err := e.ApplyEvent(evt); err != nil {
		panic("clinical: aplicar evento recién emitido: " + err.Error())
	}
	e.uncommitted = append(e.uncommitted, evt)
}

// ApplyEvent rehidrata el agregado. Un evento desconocido es error de
// programación (el registry debe registrar todos los tipos).
func (e *Encounter) ApplyEvent(evt any) error {
	switch ev := evt.(type) {
	case EncounterOpened:
		e.id = ev.EncounterID
		e.petID = ev.PetID
		e.organizationID = ev.OrganizationID
		e.appointmentID = cloneID(ev.AppointmentID)
		e.typ = ev.Type
		e.status = StatusDraft
		e.startedAt = ev.StartedAt
		e.reason = Reason(ev.Reason)
		e.attendingVetID = cloneID(ev.AttendingVetID)
		e.attachments = map[uuid.UUID]Attachment{}
		e.plan = Plan{Medications: []MedicationItem{}, Procedures: []Procedure{}}
		e.lastActivityAt = ev.StartedAt
	case EncounterDetailsUpdated:
		if ev.Type != nil {
			e.typ = *ev.Type
		}
		if ev.Reason != nil {
			e.reason = Reason(*ev.Reason)
		}
		if ev.StartedAt != nil {
			e.startedAt = *ev.StartedAt
		}
	case VitalsRecorded:
		v := ev.Vitals
		e.vitals = &v
	case EncounterNarrativeUpdated:
		if ev.Subjective != nil {
			s := Narrative(*ev.Subjective)
			e.subjective = &s
		}
		if ev.Objective != nil {
			o := cloneObjective(*ev.Objective)
			e.objective = &o
		}
	case DiagnosesSet:
		e.diagnoses = append([]Diagnosis(nil), ev.Diagnoses...)
	case PlanSet:
		e.plan = clonePlan(ev.Plan)
	case FollowUpRecommended:
		e.followUp = &FollowUpRecommendation{DueDate: kernel.CivilDate(ev.DueDate), Reason: ev.Reason}
	case FollowUpCleared:
		e.followUp = nil
	case AttendingVetAssigned:
		u := ev.UserID
		e.attendingVetID = &u
	case AttachmentAdded:
		if e.attachments == nil {
			e.attachments = map[uuid.UUID]Attachment{}
		}
		e.attachments[ev.Attachment.ID] = ev.Attachment
	case AttachmentRemoved:
		delete(e.attachments, ev.AttachmentID)
	case AttachmentVisibilityChanged:
		if a, ok := e.attachments[ev.AttachmentID]; ok {
			a.VisibleToGuardian = ev.Visible
			e.attachments[ev.AttachmentID] = a
		}
	case EncounterSigned:
		e.status = StatusSigned
		e.signature = &Signature{
			SignedBy:      ev.SignedBy,
			SignedAt:      ev.SignedAt,
			StreamVersion: ev.StreamVersion,
			ContentHash:   ev.ContentHash,
		}
		e.lastActivityAt = ev.SignedAt
	case EncounterAddendumAdded:
		e.addenda = append(e.addenda, Addendum{
			Seq: ev.Seq, Text: ev.Text, Reason: ev.Reason, SignedBy: ev.SignedBy,
			SignedAt: ev.SignedAt, Attachments: append([]Attachment(nil), ev.Attachments...),
		})
		e.lastActivityAt = ev.SignedAt
	case EncounterDiscarded:
		e.status = StatusDiscarded
		e.discardReason = ev.Reason
	default:
		return kernel.NewDomainError("UNKNOWN_EVENT", "evento desconocido para Encounter")
	}
	e.version++
	return nil
}

func cloneID(p *uuid.UUID) *uuid.UUID {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneObjective(o Objective) Objective {
	out := Objective{Narrative: o.Narrative}
	if o.Systems != nil {
		out.Systems = make(map[BodySystem]Finding, len(o.Systems))
		for k, v := range o.Systems {
			out.Systems[k] = v
		}
	}
	return out
}

func clonePlan(p Plan) Plan {
	out := Plan{
		Medications:     append([]MedicationItem(nil), p.Medications...),
		Procedures:      append([]Procedure(nil), p.Procedures...),
		Recommendations: p.Recommendations,
	}
	if out.Medications == nil {
		out.Medications = []MedicationItem{}
	}
	if out.Procedures == nil {
		out.Procedures = []Procedure{}
	}
	return out
}

// HandleCommand ejecuta un comando de dominio (docs/phase-2/02 §3.1).
func (e *Encounter) HandleCommand(cmd any) ([]any, error) {
	switch c := cmd.(type) {
	case OpenEncounter:
		return e.handleOpen(c)
	case UpdateEncounterDetails:
		return e.handleUpdateDetails(c)
	case RecordVitals:
		return e.handleVitals(c)
	case UpdateNarrative:
		return e.handleNarrative(c)
	case SetDiagnoses:
		return e.handleDiagnoses(c)
	case SetPlan:
		return e.handlePlan(c)
	case RecommendFollowUp:
		return e.handleFollowUp(c)
	case ClearFollowUp:
		return e.handleClearFollowUp(c)
	case AssignAttendingVet:
		return e.handleAssignVet(c)
	case AddAttachment:
		return e.handleAddAttachment(c)
	case RemoveAttachment:
		return e.handleRemoveAttachment(c)
	case SetTutorVisibility:
		return e.handleVisibility(c)
	case SignEncounter:
		return e.handleSign(c)
	case AddAddendum:
		return e.handleAddendum(c)
	case DiscardEncounter:
		return e.handleDiscard(c)
	}
	return nil, kernel.NewDomainError("UNKNOWN_COMMAND", "comando desconocido para Encounter")
}

// requireDraft es E3/E9: tras la firma (o el descarte) solo AddAddendum y
// SetTutorVisibility. Un stream inexistente se comporta como recurso no
// visible (404): el handler ya no distingue "no existe" de "sin permiso".
func (e *Encounter) requireDraft() error {
	if e.version == 0 {
		return &kernel.NotFoundError{Resource: "encounter"}
	}
	if e.status != StatusDraft {
		return ErrEncounterNotDraft
	}
	return nil
}

// requireVet es E4.
func (e *Encounter) requireVet(actor Actor) error {
	if actor.isVet() || actor.SuperAdmin {
		return nil
	}
	return ErrRoleRequiredVeterinarian
}

func isWithinStartWindow(t, now time.Time) bool {
	return !t.Before(now.Add(-StartWindowPast)) && !t.After(now.Add(StartWindowFuture))
}

// ------------------------------------------------------------------ open

func (e *Encounter) handleOpen(c OpenEncounter) ([]any, error) {
	if e.version != 0 {
		return nil, ErrEncounterAlreadyOpened
	}
	// E1: el puerto PetAccess ya resolvió el vínculo activo de la org.
	if !c.PetLinked {
		return nil, ErrPetNotAccessible
	}
	if c.AppointmentID != nil && !c.AppointmentCheckedIn {
		return nil, ErrAppointmentNotCheckedIn
	}
	ve := &kernel.ValidationError{}
	typ, err := NewEncounterType(c.Type)
	if err != nil {
		ve.Add("type", "INVALID")
	}
	reason, err := NewReason(c.Reason)
	if err != nil {
		ve.Add("reason", "INVALID")
	}
	now := c.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	started := c.StartedAt
	if started.IsZero() {
		started = now
	}
	// E2.
	if !isWithinStartWindow(started, now) {
		ve.Add("started_at", "OUT_OF_WINDOW")
	}
	if c.EncounterID == uuid.Nil {
		ve.Add("encounter_id", "INVALID")
	}
	if c.PetID == uuid.Nil {
		ve.Add("pet_id", "INVALID")
	}
	if c.OrganizationID == uuid.Nil {
		ve.Add("organization_id", "INVALID")
	}
	// E1: el responsable debe ser veterinario activo de la organización.
	if c.AttendingVetID != nil && !c.AttendingVetEligible {
		ve.Add("attending_vet_id", "NOT_ELIGIBLE")
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	e.record(EncounterOpened{
		EncounterID: c.EncounterID, PetID: c.PetID, OrganizationID: c.OrganizationID,
		AppointmentID: cloneID(c.AppointmentID), Type: typ, Reason: string(reason),
		StartedAt: started, AttendingVetID: cloneID(c.AttendingVetID), OpenedBy: c.Actor.UserID,
	})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ details

func (e *Encounter) handleUpdateDetails(c UpdateEncounterDetails) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	ev := EncounterDetailsUpdated{EncounterID: e.id}
	if c.Type != nil {
		typ, err := NewEncounterType(*c.Type)
		if err != nil {
			ve.Add("type", "INVALID")
		} else if typ != e.typ {
			ev.Type = &typ
		}
	}
	if c.Reason != nil {
		reason, err := NewReason(*c.Reason)
		if err != nil {
			ve.Add("reason", "INVALID")
		} else if reason != e.reason {
			r := string(reason)
			ev.Reason = &r
		}
	}
	if c.StartedAt != nil {
		now := c.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		// PRD-001: editable ±24 h respecto de ahora.
		if c.StartedAt.After(now.Add(EditWindow)) || c.StartedAt.Before(now.Add(-EditWindow)) {
			ve.Add("started_at", "OUT_OF_WINDOW")
		} else if !c.StartedAt.Equal(e.startedAt) {
			t := *c.StartedAt
			ev.StartedAt = &t
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if ev.Type == nil && ev.Reason == nil && ev.StartedAt == nil {
		return nil, nil // sin cambios (autoguardado idempotente)
	}
	e.record(ev)
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ vitals

func (e *Encounter) handleVitals(c RecordVitals) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	if c.Vitals.IsEmpty() {
		ve.Add("vitals", "EMPTY")
	}
	c.Vitals.Validate(ve, "vitals.")
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if e.vitals != nil && reflect.DeepEqual(*e.vitals, c.Vitals) {
		return nil, nil // autoguardado idempotente
	}
	e.record(VitalsRecorded{EncounterID: e.id, Vitals: c.Vitals})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ narrative

func (e *Encounter) handleNarrative(c UpdateNarrative) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	ev := EncounterNarrativeUpdated{EncounterID: e.id}
	if c.Subjective != nil {
		n, err := NewNarrative(*c.Subjective)
		if err != nil {
			ve.Add("subjective", "INVALID")
		} else if e.subjective == nil || string(*e.subjective) != string(n) {
			s := string(n)
			ev.Subjective = &s
		}
	}
	if c.Objective != nil {
		o := c.Objective.normalize(ve)
		currentEmpty := e.objective == nil || e.objective.IsEmpty()
		if !(currentEmpty && o.IsEmpty()) && (e.objective == nil || !reflect.DeepEqual(cloneObjective(*e.objective), o)) {
			ev.Objective = &o
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if ev.Subjective == nil && ev.Objective == nil {
		return nil, nil // sin cambios
	}
	e.record(ev)
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ diagnoses

func (e *Encounter) handleDiagnoses(c SetDiagnoses) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	if len(c.Diagnoses) > MaxDiagnoses {
		ve.Add("diagnoses", "TOO_MANY")
	}
	out := make([]Diagnosis, 0, len(c.Diagnoses))
	for i, d := range c.Diagnoses {
		out = append(out, d.normalize(ve, fieldIdx("diagnoses", i)))
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(e.diagnoses, out) && len(e.diagnoses) == len(out) {
		return nil, nil
	}
	e.record(DiagnosesSet{EncounterID: e.id, Diagnoses: out})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ plan

func (e *Encounter) handlePlan(c SetPlan) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	plan := c.Plan.normalize(ve)
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(clonePlan(e.plan), plan) {
		return nil, nil
	}
	e.record(PlanSet{EncounterID: e.id, Plan: plan})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ follow-up

func (e *Encounter) handleFollowUp(c RecommendFollowUp) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	due, err := kernel.NewCivilDate(c.DueDate)
	if err != nil {
		ve.Add("due_date", "INVALID")
	} else {
		now := c.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		today := kernel.CivilDateOf(now)
		if !due.After(today) || due.After(today.AddDays(MaxFollowUpDays)) {
			ve.Add("due_date", "OUT_OF_WINDOW")
		}
	}
	reason, err := cleanText(c.Reason, MaxFollowUpReasonLen, false)
	if err != nil || reason == "" {
		ve.Add("reason", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if e.followUp != nil && e.followUp.DueDate == due && e.followUp.Reason == reason {
		return nil, nil
	}
	e.record(FollowUpRecommended{EncounterID: e.id, DueDate: string(due), Reason: reason})
	return e.uncommitted, nil
}

func (e *Encounter) handleClearFollowUp(c ClearFollowUp) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	if e.followUp == nil {
		return nil, nil // idempotente
	}
	e.record(FollowUpCleared{EncounterID: e.id})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ vet

func (e *Encounter) handleAssignVet(c AssignAttendingVet) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if c.UserID == uuid.Nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "attending_vet_id", Code: "INVALID"}}}
	}
	if !c.Eligible {
		return nil, ErrAttendingVetNotEligible
	}
	if e.attendingVetID != nil && *e.attendingVetID == c.UserID {
		return nil, nil // idempotente
	}
	e.record(AttendingVetAssigned{EncounterID: e.id, UserID: c.UserID})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ attachments

func (e *Encounter) handleAddAttachment(c AddAttachment) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	a := c.Attachment.normalize(ve, "attachment", e.petID, e.id)
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if _, dup := e.attachments[a.ID]; dup {
		return nil, nil // reintento idempotente
	}
	if len(e.attachments) >= MaxAttachments {
		return nil, ErrAttachmentLimit
	}
	e.record(AttachmentAdded{EncounterID: e.id, Attachment: a})
	return e.uncommitted, nil
}

func (e *Encounter) handleRemoveAttachment(c RemoveAttachment) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if _, ok := e.attachments[c.AttachmentID]; !ok {
		return nil, ErrAttachmentNotFound
	}
	e.record(AttachmentRemoved{EncounterID: e.id, AttachmentID: c.AttachmentID})
	return e.uncommitted, nil
}

func (e *Encounter) handleVisibility(c SetTutorVisibility) ([]any, error) {
	if e.version == 0 {
		return nil, &kernel.NotFoundError{Resource: "encounter"}
	}
	if e.status == StatusDiscarded {
		return nil, ErrEncounterNotDraft
	}
	// E9: el único cambio permitido tras firmar además de la adenda.
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	a, ok := e.attachments[c.AttachmentID]
	if !ok {
		return nil, ErrAttachmentNotFound
	}
	ve := &kernel.ValidationError{}
	reason := ""
	if !c.Visible {
		r, err := cleanText(c.Reason, MaxVisibilityReasonLn, false)
		if err != nil || r == "" {
			ve.Add("reason", "INVALID")
		}
		reason = r
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if a.VisibleToGuardian == c.Visible {
		return nil, nil // idempotente
	}
	e.record(AttachmentVisibilityChanged{
		EncounterID: e.id, AttachmentID: c.AttachmentID, Visible: c.Visible, Reason: reason,
	})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ sign

func (e *Encounter) handleSign(c SignEncounter) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	// E5: solo firma el profesional responsable.
	if e.attendingVetID == nil {
		return nil, &IncompleteError{Missing: []string{MissingAttendingVet}}
	}
	if *e.attendingVetID != c.Actor.UserID {
		return nil, ErrNotAttendingVet
	}
	now := c.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// E6: contenido mínimo.
	missing := make([]string, 0, 2)
	if len(e.diagnoses) == 0 && (e.reason == "" || e.objective.IsEmpty()) {
		missing = append(missing, MissingContent)
	}
	// E7: no se firma en el futuro.
	if e.startedAt.After(now) {
		missing = append(missing, MissingStartedAt)
	}
	if len(missing) > 0 {
		return nil, &IncompleteError{Missing: missing}
	}
	if e.startedAt.IsZero() {
		return nil, &IncompleteError{Missing: []string{MissingStartedAt}}
	}
	// E8: el hash lo calcula el repositorio sobre los eventos persistidos.
	// El agregado exige que corresponda a la versión que se está firmando y
	// que tenga forma de SHA-256 hex; el valor se verifica al leer (API-013).
	if c.StreamVersion != e.version || len(c.ContentHash) != 64 || strings.Trim(c.ContentHash, "0123456789abcdef") != "" {
		return nil, kernel.NewDomainError("CONTENT_HASH_MISMATCH", "El hash de contenido no corresponde a la versión firmada.")
	}
	e.record(EncounterSigned{
		EncounterID: e.id, SignedBy: c.Actor.UserID, SignedAt: now,
		StreamVersion: c.StreamVersion, ContentHash: c.ContentHash,
		PetID: e.petID, OrganizationID: e.organizationID, AppointmentID: cloneID(e.appointmentID),
	})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ addendum

func (e *Encounter) handleAddendum(c AddAddendum) ([]any, error) {
	// E9: la adenda exige un encuentro firmado.
	if e.status != StatusSigned {
		return nil, ErrEncounterNotSigned
	}
	if err := e.requireVet(c.Actor); err != nil {
		return nil, err
	}
	ve := &kernel.ValidationError{}
	text, err := cleanText(c.Text, MaxNarrativeLen, true)
	if err != nil || text == "" {
		ve.Add("text", "INVALID")
	}
	reason := AddendumReason(strings.TrimSpace(c.Reason))
	switch reason {
	case AddendumCorrection, AddendumAdditionalFindings, AddendumLabResults, AddendumOther:
	default:
		ve.Add("reason", "INVALID")
	}
	if len(c.Attachments) > MaxAddendumAttach {
		return nil, ErrAttachmentLimit
	}
	seen := map[uuid.UUID]bool{}
	// Un adjunto ya usado en el cuerpo o en otra adenda no se repite.
	for id := range e.attachments {
		seen[id] = true
	}
	for _, ad := range e.addenda {
		for _, a := range ad.Attachments {
			seen[a.ID] = true
		}
	}
	atts := make([]Attachment, 0, len(c.Attachments))
	for i, a := range c.Attachments {
		a = a.normalize(ve, fieldIdx("attachments", i), e.petID, e.id)
		if a.ID != uuid.Nil && seen[a.ID] {
			ve.Add(fieldIdx("attachments", i)+".id", "DUPLICATE")
		}
		seen[a.ID] = true
		atts = append(atts, a)
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if len(e.addenda) >= MaxAddenda {
		return nil, ErrAddendaLimit
	}
	now := c.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	e.record(EncounterAddendumAdded{
		EncounterID: e.id, Seq: len(e.addenda) + 1, Text: text, Reason: reason,
		SignedBy: c.Actor.UserID, SignedAt: now, Attachments: atts,
	})
	return e.uncommitted, nil
}

// ------------------------------------------------------------------ discard

func (e *Encounter) handleDiscard(c DiscardEncounter) ([]any, error) {
	if err := e.requireDraft(); err != nil {
		return nil, err
	}
	// SEC-001: clinic_admin o el profesional responsable (o super_admin).
	allowed := c.Actor.SuperAdmin || c.Actor.OrgRole == RoleClinicAdmin ||
		(c.Actor.isVet() && e.attendingVetID != nil && *e.attendingVetID == c.Actor.UserID)
	if !allowed {
		return nil, ErrDiscardNotAllowed
	}
	reason, err := cleanText(c.Reason, MaxReasonLen, false)
	if err != nil || reason == "" {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reason", Code: "INVALID"}}}
	}
	e.record(EncounterDiscarded{EncounterID: e.id, Reason: reason, By: c.Actor.UserID})
	return e.uncommitted, nil
}
