package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func baseConfig() AgendaConfig {
	return AgendaConfig{
		TimeZone: "America/Bogota",
		WeeklyTemplate: map[string][]TimeRange{
			"mon": {{Start: "08:00", End: "12:00"}, {Start: "14:00", End: "18:00"}},
			"tue": {{Start: "08:00", End: "12:00"}},
			"wed": {{Start: "08:00", End: "12:00"}},
		},
		SlotMinutes: map[AppointmentType]int{TypeConsultation: 30, TypeVaccination: 15},
	}
}

func newAgenda(t *testing.T) *ProfessionalAgenda {
	t.Helper()
	a := &ProfessionalAgenda{}
	if _, err := a.HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: baseConfig(), ProfessionalOK: true, By: recepID, Now: now}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAgenda_DefineAndDefaults(t *testing.T) {
	a := newAgenda(t)
	c := a.Config()
	if c.LeadMinHours != DefaultLeadMinH || c.LeadMaxDays != DefaultLeadMaxDays || c.CancelPolicyHrs != DefaultCancelHours {
		t.Fatalf("defaults: %+v", c)
	}
	if !a.Active() || a.Version() != 1 {
		t.Fatal("agenda activa, versión 1")
	}
	if _, err := a.HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: baseConfig(), ProfessionalOK: true}); err == nil {
		t.Fatal("redefinir debe fallar")
	}
	if _, err := (&ProfessionalAgenda{}).HandleCommand(DefineAgenda{Config: baseConfig(), ProfessionalOK: false}); !errors.Is(err, ErrInvalidProfessional) {
		t.Fatalf("profesional no válido: %v", err)
	}
}

func TestAgenda_Validation(t *testing.T) {
	for name, mut := range map[string]func(*AgendaConfig){
		"tz":           func(c *AgendaConfig) { c.TimeZone = "Marte/Olimpo" },
		"solape":       func(c *AgendaConfig) { c.WeeklyTemplate["mon"] = []TimeRange{{"08:00", "12:00"}, {"11:00", "13:00"}} },
		"inicio>=fin":  func(c *AgendaConfig) { c.WeeklyTemplate["mon"] = []TimeRange{{"12:00", "08:00"}} },
		"hora mala":    func(c *AgendaConfig) { c.WeeklyTemplate["mon"] = []TimeRange{{"8:00", "12:00"}} },
		"día mal":      func(c *AgendaConfig) { c.WeeklyTemplate["lun"] = []TimeRange{{"08:00", "12:00"}} },
		"7 rangos":     func(c *AgendaConfig) { c.WeeklyTemplate["mon"] = sevenRanges() },
		"duración 4":   func(c *AgendaConfig) { c.SlotMinutes[TypeConsultation] = 4 },
		"duración 481": func(c *AgendaConfig) { c.SlotMinutes[TypeConsultation] = 481 },
		"tipo":         func(c *AgendaConfig) { c.SlotMinutes["otra"] = 30 },
		"lead max":     func(c *AgendaConfig) { c.LeadMinHours, c.LeadMaxDays, c.CancelPolicyHrs = 2, 0, 4 },
	} {
		c := baseConfig()
		mut(&c)
		_, err := NormalizeConfig(c)
		var ve *kernel.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: se esperaba ValidationError, got %v", name, err)
		}
	}
}

func sevenRanges() []TimeRange {
	return []TimeRange{{"01:00", "02:00"}, {"03:00", "04:00"}, {"05:00", "06:00"}, {"07:00", "08:00"}, {"09:00", "10:00"}, {"11:00", "12:00"}, {"13:00", "14:00"}}
}

func TestAgenda_ExceptionsAndIdempotentUpdates(t *testing.T) {
	a := newAgenda(t)
	evs, err := a.HandleCommand(UpdateWeeklyTemplate{WeeklyTemplate: baseConfig().WeeklyTemplate, By: recepID, Now: now})
	if err != nil || len(evs) != 0 {
		t.Fatalf("misma plantilla no emite: %v %v", evs, err)
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-11-09", Exception: DayException{Closed: true}, By: recepID, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-11-10", Exception: DayException{Ranges: []TimeRange{{"10:00", "13:00"}}}, By: recepID, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-11-11"}); err == nil {
		t.Fatal("excepción abierta sin rangos debe fallar")
	}
	if _, err := a.HandleCommand(RemoveException{Date: "2026-12-31"}); err == nil {
		t.Fatal("quitar una excepción inexistente → 404")
	}
	if _, err := a.HandleCommand(RemoveException{Date: "2026-11-09", By: recepID, Now: now}); err != nil {
		t.Fatal(err)
	}
	if evs, _ := a.HandleCommand(DeactivateAgenda{By: recepID, Now: now}); len(evs) != 1 || a.Active() {
		t.Fatal("desactivar")
	}
	if evs, _ := a.HandleCommand(DeactivateAgenda{By: recepID, Now: now}); len(evs) != 0 {
		t.Fatal("desactivar dos veces no emite")
	}
	if evs, _ := a.HandleCommand(ActivateAgenda{By: recepID, Now: now}); len(evs) != 1 || !a.Active() {
		t.Fatal("reactivar")
	}
	if _, err := (&ProfessionalAgenda{}).HandleCommand(SetPolicies{LeadMinHours: 1, LeadMaxDays: 10, CancelHours: 2}); err == nil {
		t.Fatal("comando sobre agenda inexistente")
	}
}

// El lunes 2026-11-02 (hora Bogotá, UTC−5) es día hábil con dos rangos.
func TestAvailability_TemplateExceptionsAndReservations(t *testing.T) {
	a := newAgenda(t)
	bog, _ := time.LoadLocation("America/Bogota")
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, bog)
	to := from.AddDate(0, 0, 1)
	query := func(mut func(*AvailabilityQuery)) []AvailableSlot {
		q := AvailabilityQuery{Type: TypeConsultation, From: from, To: to, Now: from.Add(-24 * time.Hour)}
		if mut != nil {
			mut(&q)
		}
		return a.Availability(q)
	}
	got := query(nil)
	if len(got) != 16 { // 4 h + 4 h en franjas de 30 min
		t.Fatalf("franjas = %d, want 16", len(got))
	}
	if !got[0].Start.Equal(time.Date(2026, 11, 2, 8, 0, 0, 0, bog)) {
		t.Fatalf("primera franja = %v", got[0].Start)
	}
	if n := len(query(func(q *AvailabilityQuery) { q.Type = TypeVaccination })); n != 32 {
		t.Fatalf("vacunación 15 min = %d, want 32", n)
	}
	res := Reservation{Start: time.Date(2026, 11, 2, 9, 0, 0, 0, bog), End: time.Date(2026, 11, 2, 10, 0, 0, 0, bog)}
	if n := len(query(func(q *AvailabilityQuery) { q.Reserved = []Reservation{res} })); n != 14 {
		t.Fatalf("con reserva de 1 h = %d, want 14", n)
	}
	if n := len(query(func(q *AvailabilityQuery) { q.Now = time.Date(2026, 11, 2, 11, 0, 0, 0, bog) })); n != 10 {
		t.Fatalf("solo futuro (desde 11:00) = %d, want 10 (11:00 y 11:30, más 8 de la tarde)", n)
	}
	// Excepción: cerrado y horario especial.
	_, _ = a.HandleCommand(AddException{Date: "2026-11-02", Exception: DayException{Closed: true}, Now: now})
	if n := len(query(nil)); n != 0 {
		t.Fatalf("día cerrado = %d", n)
	}
	_, _ = a.HandleCommand(AddException{Date: "2026-11-02", Exception: DayException{Ranges: []TimeRange{{"10:00", "11:00"}}}, Now: now})
	if n := len(query(nil)); n != 2 {
		t.Fatalf("horario especial = %d, want 2", n)
	}
}

func TestAvailability_GuardianLeadWindowAndInactive(t *testing.T) {
	a := newAgenda(t)
	bog, _ := time.LoadLocation("America/Bogota")
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, bog)
	nowT := time.Date(2026, 11, 2, 7, 0, 0, 0, bog) // lead min = 2 h → desde 09:00
	q := AvailabilityQuery{Type: TypeConsultation, From: from, To: from.AddDate(0, 0, 1), Now: nowT, Guardian: true}
	got := a.Availability(q)
	if len(got) == 0 || got[0].Start.Before(nowT.Add(2*time.Hour)) {
		t.Fatalf("el tutor no debe ver antes de now+2h: %v", got)
	}
	q.From, q.To = from.AddDate(0, 0, 70), from.AddDate(0, 0, 71)
	if n := len(a.Availability(q)); n != 0 {
		t.Fatalf("más allá de 60 días: %d", n)
	}
	_, _ = a.HandleCommand(DeactivateAgenda{Now: now})
	q.From, q.To = from, from.AddDate(0, 0, 1)
	if n := len(a.Availability(q)); n != 0 {
		t.Fatal("agenda inactiva no ofrece franjas")
	}
}

// Cambio de horario de verano: Nueva York 2026-11-01 (retroceso). La plantilla
// local 09:00–10:00 sigue dando 2 franjas y cada una en su hora local.
func TestAvailability_DST(t *testing.T) {
	cfg := baseConfig()
	cfg.TimeZone = "America/New_York"
	cfg.WeeklyTemplate = map[string][]TimeRange{"sun": {{Start: "09:00", End: "10:00"}}, "sat": {{Start: "09:00", End: "10:00"}}}
	a := &ProfessionalAgenda{}
	if _, err := a.HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: cfg, ProfessionalOK: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	ny, _ := time.LoadLocation("America/New_York")
	for _, day := range []int{31, 1} { // sábado 31-oct (EDT) y domingo 1-nov (EST)
		from := time.Date(2026, time.October, day, 0, 0, 0, 0, ny)
		if day == 1 {
			from = time.Date(2026, time.November, 1, 0, 0, 0, 0, ny)
		}
		got := a.Availability(AvailabilityQuery{Type: TypeConsultation, From: from, To: from.Add(26 * time.Hour), Now: from.AddDate(0, 0, -3)})
		if len(got) != 2 {
			t.Fatalf("día %d: %d franjas, want 2", day, len(got))
		}
		h := got[0].Start.In(ny).Hour()
		if h != 9 {
			t.Fatalf("día %d: la primera franja empieza a las %d h locales, want 9", day, h)
		}
	}
}

func TestAgenda_Covers(t *testing.T) {
	a := newAgenda(t)
	bog, _ := time.LoadLocation("America/Bogota")
	at := func(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, bog) }
	for name, tc := range map[string]struct {
		s    Slot
		want bool
	}{
		"dentro":          {Slot{at(8, 0).UTC(), at(8, 30).UTC()}, true},
		"borde final":     {Slot{at(11, 30).UTC(), at(12, 0).UTC()}, true},
		"cruza el rango":  {Slot{at(11, 45).UTC(), at(12, 15).UTC()}, false},
		"en el hueco":     {Slot{at(12, 0).UTC(), at(12, 30).UTC()}, false},
		"día sin horario": {Slot{time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 15, 30, 0, 0, time.UTC)}, false},
	} {
		if got := a.Covers(tc.s); got != tc.want {
			t.Errorf("%s: Covers=%v, want %v", name, got, tc.want)
		}
	}
}

func TestAgenda_SnapshotRoundTrip(t *testing.T) {
	a := newAgenda(t)
	_, _ = a.HandleCommand(AddException{Date: "2026-11-09", Exception: DayException{Closed: true}, Now: now})
	_, _ = a.HandleCommand(SetPolicies{LeadMinHours: 4, LeadMaxDays: 30, CancelHours: 12, Now: now})
	state, schema := a.SnapshotState()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	b := &ProfessionalAgenda{}
	if err := b.RestoreSnapshot(raw, schema); err != nil {
		t.Fatal(err)
	}
	b.SetVersion(a.Version())
	if b.Version() != a.Version() || b.Active() != a.Active() || b.Config().LeadMaxDays != 30 || len(b.Exceptions()) != 1 {
		t.Fatalf("snapshot distinto: %+v", b.Config())
	}
	// Rehidratar snapshot + evento posterior == replay completo.
	if _, err := b.HandleCommand(DeactivateAgenda{Now: now}); err != nil {
		t.Fatal(err)
	}
	if b.Active() || b.Version() != a.Version()+1 {
		t.Fatal("eventos posteriores al snapshot")
	}
	if err := (&ProfessionalAgenda{}).RestoreSnapshot(raw, schema+1); err == nil {
		t.Fatal("schema incompatible debe rechazarse")
	}
	_ = uuid.Nil
}
