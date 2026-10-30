package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
	_ "time/tzdata" // zonas IANA embebidas: no dependen del SO (Windows, imágenes mínimas)

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AgendaStreamType es el tipo de stream de la agenda (agenda-<Org>-<User>).
const AgendaStreamType = "ProfessionalAgenda"

// SnapshotThreshold: cada cuántos eventos se escribe un snapshot (ARQ-004).
const SnapshotThreshold = 100

// agendaSnapshotSchema es la versión del estado serializado del snapshot.
const agendaSnapshotSchema = 1

// Valores por defecto y rangos de las políticas (02 §4.2).
const (
	DefaultSlotMinutes = 30
	DefaultLeadMinH    = 2
	DefaultLeadMaxDays = 60
	DefaultCancelHours = 4
	MaxRangesPerDay    = 6
	MaxLeadMinHours    = 24 * 14
	MaxLeadMaxDays     = 365
	MaxCancelHours     = 24 * 7
	MaxExceptions      = 400
)

// Weekdays son las claves de la plantilla semanal.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var weekdayKey = map[time.Weekday]string{
	time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed", time.Thursday: "thu",
	time.Friday: "fri", time.Saturday: "sat", time.Sunday: "sun",
}

// TimeRange es un rango horario local "HH:MM"–"HH:MM" (Start < End).
type TimeRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func parseHM(s string) (int, bool) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// Minutes devuelve los minutos desde medianoche de inicio y fin.
func (r TimeRange) Minutes() (start, end int, ok bool) {
	s, ok1 := parseHM(r.Start)
	e, ok2 := parseHM(r.End)
	return s, e, ok1 && ok2 && s < e
}

// DayException cambia un día concreto: cerrado o con horario especial.
type DayException struct {
	Closed bool        `json:"closed"`
	Ranges []TimeRange `json:"ranges,omitempty"`
}

// AgendaConfig es la configuración completa que cambia con el PUT de la API.
type AgendaConfig struct {
	TimeZone        string                  `json:"tz"`
	WeeklyTemplate  map[string][]TimeRange  `json:"weekly_template"`
	SlotMinutes     map[AppointmentType]int `json:"slot_minutes"`
	LeadMinHours    int                     `json:"lead_time_min_hours"`
	LeadMaxDays     int                     `json:"lead_time_max_days"`
	CancelPolicyHrs int                     `json:"cancellation_policy_hours"`
}

// ProfessionalAgenda es la raíz de agregado de disponibilidad de un
// profesional en una organización (PP-F2-DOM-011). No contiene citas.
type ProfessionalAgenda struct {
	orgID       uuid.UUID
	profID      uuid.UUID
	version     int64
	defined     bool
	active      bool
	cfg         AgendaConfig
	exceptions  map[string]DayException
	uncommitted []any
}

// Organization devuelve la organización.
func (a *ProfessionalAgenda) Organization() uuid.UUID { return a.orgID }

// Professional devuelve el profesional.
func (a *ProfessionalAgenda) Professional() uuid.UUID { return a.profID }

// Version devuelve la versión del stream.
func (a *ProfessionalAgenda) Version() int64 { return a.version }

// Defined indica si la agenda existe.
func (a *ProfessionalAgenda) Defined() bool { return a.defined }

// Active indica si la agenda admite citas.
func (a *ProfessionalAgenda) Active() bool { return a.active }

// Config devuelve la configuración vigente.
func (a *ProfessionalAgenda) Config() AgendaConfig { return a.cfg }

// Exceptions devuelve las excepciones por fecha (YYYY-MM-DD).
func (a *ProfessionalAgenda) Exceptions() map[string]DayException { return a.exceptions }

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (a *ProfessionalAgenda) UncommittedEvents() []any { return a.uncommitted }

// MarkCommitted limpia los eventos tras el Append.
func (a *ProfessionalAgenda) MarkCommitted() { a.uncommitted = nil }

func (a *ProfessionalAgenda) record(evt any) {
	if err := a.ApplyEvent(evt); err != nil {
		panic("scheduling: aplicar evento de agenda recién emitido: " + err.Error())
	}
	a.uncommitted = append(a.uncommitted, evt)
}

// SlotMinutesFor devuelve la duración de franja del tipo (30 por defecto).
func (a *ProfessionalAgenda) SlotMinutesFor(t AppointmentType) int {
	if m, ok := a.cfg.SlotMinutes[t]; ok && m > 0 {
		return m
	}
	return DefaultSlotMinutes
}

// ------------------------------------------------------------ snapshots

type agendaSnapshot struct {
	OrgID      uuid.UUID               `json:"org_id"`
	ProfID     uuid.UUID               `json:"professional_id"`
	Defined    bool                    `json:"defined"`
	Active     bool                    `json:"active"`
	Config     AgendaConfig            `json:"config"`
	Exceptions map[string]DayException `json:"exceptions"`
}

var _ eventstore.Snapshotter = (*ProfessionalAgenda)(nil)

// SnapshotState implementa eventstore.Snapshotter.
func (a *ProfessionalAgenda) SnapshotState() (any, int) {
	return agendaSnapshot{
		OrgID: a.orgID, ProfID: a.profID, Defined: a.defined, Active: a.active,
		Config: a.cfg, Exceptions: a.exceptions,
	}, agendaSnapshotSchema
}

// RestoreSnapshot implementa eventstore.Snapshotter. La versión del stream la
// fija el repositorio con SetVersion tras restaurar.
func (a *ProfessionalAgenda) RestoreSnapshot(state json.RawMessage, schemaVersion int) error {
	if schemaVersion != agendaSnapshotSchema {
		return fmt.Errorf("scheduling: snapshot de agenda con schema %d no compatible", schemaVersion)
	}
	var s agendaSnapshot
	if err := json.Unmarshal(state, &s); err != nil {
		return fmt.Errorf("scheduling: snapshot de agenda ilegible: %w", err)
	}
	a.orgID, a.profID, a.defined, a.active, a.cfg = s.OrgID, s.ProfID, s.Defined, s.Active, s.Config
	a.exceptions = s.Exceptions
	if a.exceptions == nil {
		a.exceptions = map[string]DayException{}
	}
	return nil
}

// SetVersion fija la versión tras restaurar un snapshot.
func (a *ProfessionalAgenda) SetVersion(v int64) { a.version = v }

// --------------------------------------------------------------- eventos

// AgendaRef identifica la agenda en los eventos.
type AgendaRef struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	ProfessionalID uuid.UUID `json:"professional_id"`
}

// AgendaDefined: se crea la agenda con su configuración inicial.
type AgendaDefined struct {
	AgendaRef
	Config AgendaConfig `json:"config"`
	By     uuid.UUID    `json:"by"`
	At     time.Time    `json:"at"`
}

// AgendaWeeklyTemplateUpdated: nueva plantilla semanal.
type AgendaWeeklyTemplateUpdated struct {
	AgendaRef
	WeeklyTemplate map[string][]TimeRange `json:"weekly_template"`
	By             uuid.UUID              `json:"by"`
	At             time.Time              `json:"at"`
}

// AgendaSlotDurationsSet: duración de franja por tipo.
type AgendaSlotDurationsSet struct {
	AgendaRef
	SlotMinutes map[AppointmentType]int `json:"slot_minutes"`
	By          uuid.UUID               `json:"by"`
	At          time.Time               `json:"at"`
}

// AgendaExceptionAdded: excepción de un día (reemplaza la anterior).
type AgendaExceptionAdded struct {
	AgendaRef
	Date      kernel.CivilDate `json:"date"`
	Exception DayException     `json:"exception"`
	By        uuid.UUID        `json:"by"`
	At        time.Time        `json:"at"`
}

// AgendaExceptionRemoved: se elimina la excepción de un día.
type AgendaExceptionRemoved struct {
	AgendaRef
	Date kernel.CivilDate `json:"date"`
	By   uuid.UUID        `json:"by"`
	At   time.Time        `json:"at"`
}

// AgendaPoliciesSet: antelación mínima/máxima y cancelación.
type AgendaPoliciesSet struct {
	AgendaRef
	LeadMinHours int       `json:"lead_time_min_hours"`
	LeadMaxDays  int       `json:"lead_time_max_days"`
	CancelHours  int       `json:"cancellation_policy_hours"`
	By           uuid.UUID `json:"by"`
	At           time.Time `json:"at"`
}

// AgendaDeactivated: la agenda deja de admitir citas.
type AgendaDeactivated struct {
	AgendaRef
	By uuid.UUID `json:"by"`
	At time.Time `json:"at"`
}

// AgendaActivated: reactivación (el PUT de la API admite active=true).
type AgendaActivated struct {
	AgendaRef
	By uuid.UUID `json:"by"`
	At time.Time `json:"at"`
}

// EventName / SchemaVersion implementan eventstore.Event.
func (AgendaDefined) EventName() string                { return "AgendaDefined" }
func (AgendaDefined) SchemaVersion() int               { return 1 }
func (AgendaWeeklyTemplateUpdated) EventName() string  { return "AgendaWeeklyTemplateUpdated" }
func (AgendaWeeklyTemplateUpdated) SchemaVersion() int { return 1 }
func (AgendaSlotDurationsSet) EventName() string       { return "AgendaSlotDurationsSet" }
func (AgendaSlotDurationsSet) SchemaVersion() int      { return 1 }
func (AgendaExceptionAdded) EventName() string         { return "AgendaExceptionAdded" }
func (AgendaExceptionAdded) SchemaVersion() int        { return 1 }
func (AgendaExceptionRemoved) EventName() string       { return "AgendaExceptionRemoved" }
func (AgendaExceptionRemoved) SchemaVersion() int      { return 1 }
func (AgendaPoliciesSet) EventName() string            { return "AgendaPoliciesSet" }
func (AgendaPoliciesSet) SchemaVersion() int           { return 1 }
func (AgendaDeactivated) EventName() string            { return "AgendaDeactivated" }
func (AgendaDeactivated) SchemaVersion() int           { return 1 }
func (AgendaActivated) EventName() string              { return "AgendaActivated" }
func (AgendaActivated) SchemaVersion() int             { return 1 }

// ApplyEvent rehidrata el agregado.
func (a *ProfessionalAgenda) ApplyEvent(evt any) error {
	if a.exceptions == nil {
		a.exceptions = map[string]DayException{}
	}
	switch ev := evt.(type) {
	case AgendaDefined:
		a.orgID, a.profID, a.defined, a.active = ev.OrganizationID, ev.ProfessionalID, true, true
		a.cfg = ev.Config
	case AgendaWeeklyTemplateUpdated:
		a.cfg.WeeklyTemplate = ev.WeeklyTemplate
	case AgendaSlotDurationsSet:
		a.cfg.SlotMinutes = ev.SlotMinutes
	case AgendaExceptionAdded:
		a.exceptions[ev.Date.String()] = ev.Exception
	case AgendaExceptionRemoved:
		delete(a.exceptions, ev.Date.String())
	case AgendaPoliciesSet:
		a.cfg.LeadMinHours, a.cfg.LeadMaxDays, a.cfg.CancelPolicyHrs = ev.LeadMinHours, ev.LeadMaxDays, ev.CancelHours
	case AgendaDeactivated:
		a.active = false
	case AgendaActivated:
		a.active = true
	default:
		return kernel.NewDomainError("UNKNOWN_EVENT", "evento desconocido para ProfessionalAgenda")
	}
	a.version++
	return nil
}

// -------------------------------------------------------------- comandos

// DefineAgenda crea la agenda. ProfessionalOK lo resuelve app con el puerto
// Membership (veterinarian activo de la organización).
type DefineAgenda struct {
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Config         AgendaConfig
	ProfessionalOK bool
	By             uuid.UUID
	Now            time.Time
}

// UpdateWeeklyTemplate reemplaza la plantilla semanal.
type UpdateWeeklyTemplate struct {
	WeeklyTemplate map[string][]TimeRange
	By             uuid.UUID
	Now            time.Time
}

// SetSlotDurations fija la duración de franja por tipo.
type SetSlotDurations struct {
	SlotMinutes map[AppointmentType]int
	By          uuid.UUID
	Now         time.Time
}

// AddException añade o reemplaza la excepción de un día.
type AddException struct {
	Date      string
	Exception DayException
	By        uuid.UUID
	Now       time.Time
}

// RemoveException elimina la excepción de un día.
type RemoveException struct {
	Date string
	By   uuid.UUID
	Now  time.Time
}

// SetPolicies fija antelaciones y política de cancelación.
type SetPolicies struct {
	LeadMinHours int
	LeadMaxDays  int
	CancelHours  int
	By           uuid.UUID
	Now          time.Time
}

// DeactivateAgenda desactiva la agenda.
type DeactivateAgenda struct {
	By  uuid.UUID
	Now time.Time
}

// ActivateAgenda reactiva la agenda.
type ActivateAgenda struct {
	By  uuid.UUID
	Now time.Time
}

// HandleCommand ejecuta un comando y devuelve los eventos emitidos.
func (a *ProfessionalAgenda) HandleCommand(cmd any) ([]any, error) {
	before := len(a.uncommitted)
	var err error
	switch x := cmd.(type) {
	case DefineAgenda:
		err = a.handleDefine(x)
	case UpdateWeeklyTemplate:
		err = a.requireDefined()
		if err == nil {
			err = a.handleTemplate(x)
		}
	case SetSlotDurations:
		err = a.requireDefined()
		if err == nil {
			err = a.handleDurations(x)
		}
	case AddException:
		err = a.requireDefined()
		if err == nil {
			err = a.handleAddException(x)
		}
	case RemoveException:
		err = a.requireDefined()
		if err == nil {
			err = a.handleRemoveException(x)
		}
	case SetPolicies:
		err = a.requireDefined()
		if err == nil {
			err = a.handlePolicies(x)
		}
	case DeactivateAgenda:
		err = a.requireDefined()
		if err == nil && a.active {
			a.record(AgendaDeactivated{AgendaRef: a.ref(), By: x.By, At: x.Now})
		}
	case ActivateAgenda:
		err = a.requireDefined()
		if err == nil && !a.active {
			a.record(AgendaActivated{AgendaRef: a.ref(), By: x.By, At: x.Now})
		}
	default:
		err = kernel.NewDomainError("UNKNOWN_COMMAND", "comando desconocido para ProfessionalAgenda")
	}
	if err != nil {
		return nil, err
	}
	return a.uncommitted[before:], nil
}

func (a *ProfessionalAgenda) ref() AgendaRef {
	return AgendaRef{OrganizationID: a.orgID, ProfessionalID: a.profID}
}

func (a *ProfessionalAgenda) requireDefined() error {
	if !a.defined {
		return &kernel.NotFoundError{Resource: "agenda"}
	}
	return nil
}

// NormalizeConfig valida y completa una configuración (valores por defecto).
func NormalizeConfig(c AgendaConfig) (AgendaConfig, error) {
	ve := &kernel.ValidationError{}
	if _, err := time.LoadLocation(c.TimeZone); err != nil || c.TimeZone == "" || c.TimeZone == "Local" {
		ve.Add("tz", "INVALID")
	}
	c.WeeklyTemplate = normalizeTemplate(c.WeeklyTemplate, ve)
	c.SlotMinutes = normalizeDurations(c.SlotMinutes, ve)
	if c.LeadMinHours == 0 && c.LeadMaxDays == 0 && c.CancelPolicyHrs == 0 {
		c.LeadMinHours, c.LeadMaxDays, c.CancelPolicyHrs = DefaultLeadMinH, DefaultLeadMaxDays, DefaultCancelHours
	}
	checkPolicies(c.LeadMinHours, c.LeadMaxDays, c.CancelPolicyHrs, ve)
	if err := ve.ErrOrNil(); err != nil {
		return AgendaConfig{}, err
	}
	return c, nil
}

func checkPolicies(minH, maxD, cancelH int, ve *kernel.ValidationError) {
	if minH < 0 || minH > MaxLeadMinHours {
		ve.Add("lead_time_min_hours", "OUT_OF_RANGE")
	}
	if maxD < 1 || maxD > MaxLeadMaxDays {
		ve.Add("lead_time_max_days", "OUT_OF_RANGE")
	}
	if cancelH < 0 || cancelH > MaxCancelHours {
		ve.Add("cancellation_policy_hours", "OUT_OF_RANGE")
	}
}

func normalizeTemplate(t map[string][]TimeRange, ve *kernel.ValidationError) map[string][]TimeRange {
	out := map[string][]TimeRange{}
	for day, ranges := range t {
		valid := false
		for _, w := range Weekdays {
			if w == day {
				valid = true
			}
		}
		if !valid {
			ve.Add("weekly_template."+day, "INVALID")
			continue
		}
		if r := checkRanges(ranges); r != "" {
			ve.Add("weekly_template."+day, r)
			continue
		}
		if len(ranges) > 0 {
			out[day] = sortedRanges(ranges)
		}
	}
	return out
}

func normalizeDurations(m map[AppointmentType]int, ve *kernel.ValidationError) map[AppointmentType]int {
	out := map[AppointmentType]int{}
	for t, mins := range m {
		if _, err := NewAppointmentType(string(t)); err != nil {
			ve.Add("slot_minutes."+string(t), "INVALID")
			continue
		}
		if mins < MinSlotMinutes || mins > MaxSlotMinutes {
			ve.Add("slot_minutes."+string(t), "OUT_OF_RANGE")
			continue
		}
		out[t] = mins
	}
	return out
}

func sortedRanges(in []TimeRange) []TimeRange {
	out := append([]TimeRange(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// checkRanges devuelve el código de la violación ("" si es válido): ≤ 6
// rangos, cada uno con Start < End y sin solapes entre sí.
func checkRanges(in []TimeRange) string {
	if len(in) > MaxRangesPerDay {
		return "TOO_MANY"
	}
	sorted := sortedRanges(in)
	prevEnd := -1
	for _, r := range sorted {
		s, e, ok := r.Minutes()
		if !ok {
			return "INVALID"
		}
		if s < prevEnd {
			return "OVERLAP"
		}
		prevEnd = e
	}
	return ""
}

func (a *ProfessionalAgenda) handleDefine(c DefineAgenda) error {
	if a.defined {
		return kernel.NewDomainError("AGENDA_EXISTS", "La agenda ya existe.")
	}
	if !c.ProfessionalOK {
		return ErrInvalidProfessional
	}
	cfg, err := NormalizeConfig(c.Config)
	if err != nil {
		return err
	}
	a.record(AgendaDefined{
		AgendaRef: AgendaRef{OrganizationID: c.OrganizationID, ProfessionalID: c.ProfessionalID},
		Config:    cfg, By: c.By, At: c.Now,
	})
	return nil
}

func (a *ProfessionalAgenda) handleTemplate(c UpdateWeeklyTemplate) error {
	ve := &kernel.ValidationError{}
	t := normalizeTemplate(c.WeeklyTemplate, ve)
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	if sameTemplate(t, a.cfg.WeeklyTemplate) {
		return nil
	}
	a.record(AgendaWeeklyTemplateUpdated{AgendaRef: a.ref(), WeeklyTemplate: t, By: c.By, At: c.Now})
	return nil
}

func sameTemplate(a, b map[string][]TimeRange) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (a *ProfessionalAgenda) handleDurations(c SetSlotDurations) error {
	ve := &kernel.ValidationError{}
	m := normalizeDurations(c.SlotMinutes, ve)
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	x, _ := json.Marshal(m)
	y, _ := json.Marshal(a.cfg.SlotMinutes)
	if string(x) == string(y) {
		return nil
	}
	a.record(AgendaSlotDurationsSet{AgendaRef: a.ref(), SlotMinutes: m, By: c.By, At: c.Now})
	return nil
}

func (a *ProfessionalAgenda) handleAddException(c AddException) error {
	ve := &kernel.ValidationError{}
	d, err := kernel.NewCivilDate(c.Date)
	if err != nil {
		ve.Add("date", "INVALID")
	}
	ex := c.Exception
	if ex.Closed {
		ex.Ranges = nil
	} else if len(ex.Ranges) == 0 {
		ve.Add("ranges", "REQUIRED")
	} else if r := checkRanges(ex.Ranges); r != "" {
		ve.Add("ranges", r)
	} else {
		ex.Ranges = sortedRanges(ex.Ranges)
	}
	if _, ok := a.exceptions[c.Date]; !ok && len(a.exceptions) >= MaxExceptions {
		ve.Add("date", "LIMIT")
	}
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	a.record(AgendaExceptionAdded{AgendaRef: a.ref(), Date: d, Exception: ex, By: c.By, At: c.Now})
	return nil
}

func (a *ProfessionalAgenda) handleRemoveException(c RemoveException) error {
	d, err := kernel.NewCivilDate(c.Date)
	if err != nil {
		ve := &kernel.ValidationError{}
		ve.Add("date", "INVALID")
		return ve
	}
	if _, ok := a.exceptions[c.Date]; !ok {
		return &kernel.NotFoundError{Resource: "exception"}
	}
	a.record(AgendaExceptionRemoved{AgendaRef: a.ref(), Date: d, By: c.By, At: c.Now})
	return nil
}

func (a *ProfessionalAgenda) handlePolicies(c SetPolicies) error {
	ve := &kernel.ValidationError{}
	checkPolicies(c.LeadMinHours, c.LeadMaxDays, c.CancelHours, ve)
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	if c.LeadMinHours == a.cfg.LeadMinHours && c.LeadMaxDays == a.cfg.LeadMaxDays && c.CancelHours == a.cfg.CancelPolicyHrs {
		return nil
	}
	a.record(AgendaPoliciesSet{AgendaRef: a.ref(), LeadMinHours: c.LeadMinHours, LeadMaxDays: c.LeadMaxDays, CancelHours: c.CancelHours, By: c.By, At: c.Now})
	return nil
}

// RestoreAgenda reconstruye una agenda de solo lectura desde el read model
// (consultas de disponibilidad): no emite eventos ni se persiste.
func RestoreAgenda(orgID, professionalID uuid.UUID, version int64, active bool, cfg AgendaConfig, exceptions map[string]DayException) *ProfessionalAgenda {
	if exceptions == nil {
		exceptions = map[string]DayException{}
	}
	return &ProfessionalAgenda{
		orgID: orgID, profID: professionalID, version: version, defined: true, active: active,
		cfg: cfg, exceptions: exceptions,
	}
}
