package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func mustCivil(t *testing.T, s string) kernel.CivilDate {
	t.Helper()
	d, err := kernel.NewCivilDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAgenda_AccessorsAndCommit(t *testing.T) {
	a := newAgenda(t)
	if a.Organization() != orgID || a.Professional() != vetID || !a.Defined() || a.Version() != 1 {
		t.Fatalf("accesores: %v %v %v %d", a.Organization(), a.Professional(), a.Defined(), a.Version())
	}
	if len(a.UncommittedEvents()) != 1 {
		t.Fatalf("eventos pendientes = %d", len(a.UncommittedEvents()))
	}
	a.MarkCommitted()
	if len(a.UncommittedEvents()) != 0 {
		t.Fatal("MarkCommitted no limpia los eventos")
	}
	if a.SlotMinutesFor(TypeConsultation) != 30 || a.SlotMinutesFor(TypeVaccination) != 15 {
		t.Fatal("duración configurada")
	}
	if a.SlotMinutesFor(AppointmentType("otro")) != DefaultSlotMinutes {
		t.Fatal("duración por defecto")
	}
}

func TestAgenda_DurationsTemplateAndPolicies(t *testing.T) {
	a := newAgenda(t)
	by := recepID

	// Duraciones: inválidas, cambio y repetición idempotente.
	if _, err := a.HandleCommand(SetSlotDurations{SlotMinutes: map[AppointmentType]int{"zzz": 30}, By: by, Now: now}); err == nil {
		t.Fatal("tipo inválido debe fallar")
	}
	if _, err := a.HandleCommand(SetSlotDurations{SlotMinutes: map[AppointmentType]int{TypeConsultation: 100000}, By: by, Now: now}); err == nil {
		t.Fatal("duración fuera de rango debe fallar")
	}
	evs, err := a.HandleCommand(SetSlotDurations{SlotMinutes: map[AppointmentType]int{TypeConsultation: 45}, By: by, Now: now})
	if err != nil || len(evs) != 1 || a.SlotMinutesFor(TypeConsultation) != 45 {
		t.Fatalf("cambio de duración: %v %v", evs, err)
	}
	evs, err = a.HandleCommand(SetSlotDurations{SlotMinutes: map[AppointmentType]int{TypeConsultation: 45}, By: by, Now: now})
	if err != nil || len(evs) != 0 {
		t.Fatalf("repetir debe ser idempotente: %v %v", evs, err)
	}

	// Plantilla: rango inválido, cambio e idempotencia.
	if _, err := a.HandleCommand(UpdateWeeklyTemplate{WeeklyTemplate: map[string][]TimeRange{"mon": {{Start: "12:00", End: "08:00"}}}, By: by, Now: now}); err == nil {
		t.Fatal("rango invertido debe fallar")
	}
	tpl := map[string][]TimeRange{"thu": {{Start: "09:00", End: "11:00"}}}
	if evs, err = a.HandleCommand(UpdateWeeklyTemplate{WeeklyTemplate: tpl, By: by, Now: now}); err != nil || len(evs) != 1 {
		t.Fatalf("plantilla: %v %v", evs, err)
	}
	if evs, err = a.HandleCommand(UpdateWeeklyTemplate{WeeklyTemplate: tpl, By: by, Now: now}); err != nil || len(evs) != 0 {
		t.Fatalf("plantilla idéntica debe ser idempotente: %v %v", evs, err)
	}

	// Políticas: fuera de rango, cambio e idempotencia.
	if _, err := a.HandleCommand(SetPolicies{LeadMinHours: -1, LeadMaxDays: 0, CancelHours: -5, By: by, Now: now}); err == nil {
		t.Fatal("políticas inválidas deben fallar")
	}
	pol := SetPolicies{LeadMinHours: 3, LeadMaxDays: 30, CancelHours: 6, By: by, Now: now}
	if evs, err = a.HandleCommand(pol); err != nil || len(evs) != 1 {
		t.Fatalf("políticas: %v %v", evs, err)
	}
	if evs, err = a.HandleCommand(pol); err != nil || len(evs) != 0 {
		t.Fatalf("políticas idénticas deben ser idempotentes: %v %v", evs, err)
	}
}

func TestAgenda_ExceptionErrors(t *testing.T) {
	a := newAgenda(t)
	by := recepID
	if _, err := a.HandleCommand(AddException{Date: "no-fecha", Exception: DayException{Closed: true}, By: by, Now: now}); err == nil {
		t.Fatal("fecha inválida debe fallar")
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-12-24", Exception: DayException{}, By: by, Now: now}); err == nil {
		t.Fatal("excepción abierta sin rangos debe fallar")
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-12-24", Exception: DayException{Ranges: []TimeRange{{Start: "10:00", End: "09:00"}}}, By: by, Now: now}); err == nil {
		t.Fatal("rango inválido en excepción debe fallar")
	}
	if _, err := a.HandleCommand(RemoveException{Date: "no-fecha", By: by, Now: now}); err == nil {
		t.Fatal("fecha inválida al eliminar debe fallar")
	}
	if _, err := a.HandleCommand(RemoveException{Date: "2026-12-25", By: by, Now: now}); err == nil {
		t.Fatal("eliminar una excepción inexistente debe fallar")
	}
	if _, err := a.HandleCommand(AddException{Date: "2026-12-24", Exception: DayException{Closed: true}, By: by, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.HandleCommand(RemoveException{Date: "2026-12-24", By: by, Now: now}); err != nil {
		t.Fatal(err)
	}
	if len(a.Exceptions()) != 0 {
		t.Fatal("la excepción debe haberse eliminado")
	}
}

func TestAgenda_DefineErrors(t *testing.T) {
	a := newAgenda(t)
	if _, err := a.HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: baseConfig(), ProfessionalOK: true, By: recepID, Now: now}); err == nil {
		t.Fatal("definir dos veces debe fallar")
	}
	if _, err := (&ProfessionalAgenda{}).HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: baseConfig(), By: recepID, Now: now}); err == nil {
		t.Fatal("profesional inválido debe fallar")
	}
	bad := baseConfig()
	bad.TimeZone = "No/Existe"
	if _, err := (&ProfessionalAgenda{}).HandleCommand(DefineAgenda{OrganizationID: orgID, ProfessionalID: vetID, Config: bad, ProfessionalOK: true, By: recepID, Now: now}); err == nil {
		t.Fatal("zona horaria inválida debe fallar")
	}
}

func TestRestoreAgenda(t *testing.T) {
	cfg := baseConfig()
	a := RestoreAgenda(orgID, vetID, 7, true, cfg, nil)
	if !a.Defined() || !a.Active() || a.Version() != 7 || a.Exceptions() == nil {
		t.Fatalf("restauración: %+v", a)
	}
	if a.Organization() != orgID || a.Professional() != vetID {
		t.Fatal("ids restaurados")
	}
}

func TestAppointment_AccessorsAndCommit(t *testing.T) {
	a := confirmed(t)
	if a.ID() != apptID || a.PetID() != petID || a.GuardianID() != guardID || a.OrganizationID() != orgID {
		t.Fatal("accesores de identidad")
	}
	if a.Type() != TypeConsultation || a.TZ() != "America/Bogota" {
		t.Fatalf("tipo/zona: %s %s", a.Type(), a.TZ())
	}
	if _, ok := a.ProposedSlot(); ok {
		t.Fatal("una cita confirmada no tiene propuesta")
	}
	if len(a.UncommittedEvents()) == 0 {
		t.Fatal("debe haber eventos pendientes")
	}
	a.MarkCommitted()
	if len(a.UncommittedEvents()) != 0 {
		t.Fatal("MarkCommitted no limpia los eventos")
	}
}

func proposed(t *testing.T) *Appointment {
	t.Helper()
	a := &Appointment{}
	mustHandle(t, a, requestCmd())
	mustHandle(t, a, ProposeAlternative{
		ProfessionalID: vetID, Start: slotStart.Add(time.Hour), End: slotEnd.Add(time.Hour),
		TZ: "America/Bogota", PolicyHours: 4, ProfessionalOK: true, By: recepID, Now: now,
	})
	return a
}

func TestAppointment_ProposalRules(t *testing.T) {
	a := proposed(t)
	p, ok := a.ProposedSlot()
	if !ok || !p.Slot.Start.Equal(slotStart.Add(time.Hour)) {
		t.Fatalf("propuesta = %+v ok=%v", p, ok)
	}

	t.Run("proponer desde un estado inválido", func(t *testing.T) {
		_, err := proposed(t).HandleCommand(ProposeAlternative{ProfessionalID: vetID, Start: slotStart, End: slotEnd, ProfessionalOK: true, Now: now})
		wantCode(t, err, "INVALID_TRANSITION")
	})
	t.Run("proponer una franja pasada", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(ProposeAlternative{ProfessionalID: vetID, Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour), ProfessionalOK: true, Now: now})
		if err == nil {
			t.Fatal("franja pasada debe fallar")
		}
	})
	t.Run("proponer una franja inválida", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(ProposeAlternative{ProfessionalID: vetID, Start: slotEnd, End: slotStart, ProfessionalOK: true, Now: now})
		if err == nil {
			t.Fatal("franja invertida debe fallar")
		}
	})
	t.Run("proponer con profesional no válido", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(ProposeAlternative{ProfessionalID: vetID, Start: slotStart, End: slotEnd, Now: now})
		wantCode(t, err, "INVALID_PROFESSIONAL")
	})
	t.Run("aceptar tras vencer", func(t *testing.T) {
		_, err := proposed(t).HandleCommand(AcceptProposal{By: guardID, Now: now.Add(ProposalTTL + time.Minute)})
		wantCode(t, err, "PROPOSAL_EXPIRED")
	})
	t.Run("aceptar sin propuesta", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(AcceptProposal{By: guardID, Now: now})
		wantCode(t, err, "INVALID_TRANSITION")
	})
	t.Run("aceptar confirma", func(t *testing.T) {
		b := proposed(t)
		mustHandle(t, b, AcceptProposal{By: guardID, Now: now.Add(time.Hour)})
		if b.Status() != StatusConfirmed {
			t.Fatalf("estado = %s", b.Status())
		}
	})
	t.Run("expirar antes de tiempo no hace nada", func(t *testing.T) {
		b := proposed(t)
		if evs := mustHandle(t, b, ExpireProposal{Now: now.Add(time.Hour)}); len(evs) != 0 {
			t.Fatalf("eventos = %v", evs)
		}
	})
	t.Run("expirar vencida rechaza", func(t *testing.T) {
		b := proposed(t)
		mustHandle(t, b, ExpireProposal{Now: now.Add(ProposalTTL + time.Minute)})
		if b.Status() != StatusRejected {
			t.Fatalf("estado = %s", b.Status())
		}
	})
}

func TestAppointment_RejectCancelRescheduleErrors(t *testing.T) {
	t.Run("rechazar sin motivo", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		if _, err := b.HandleCommand(RejectRequest{Reason: "  ", By: recepID, Now: now}); err == nil {
			t.Fatal("motivo vacío debe fallar")
		}
	})
	t.Run("rechazar con motivo", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		mustHandle(t, b, RejectRequest{Reason: "Sin cupo", By: recepID, Now: now})
		if b.Status() != StatusRejected {
			t.Fatalf("estado = %s", b.Status())
		}
	})
	t.Run("rechazar una cita confirmada", func(t *testing.T) {
		_, err := confirmed(t).HandleCommand(RejectRequest{Reason: "x", By: recepID, Now: now})
		wantCode(t, err, "INVALID_TRANSITION")
	})
	t.Run("cancelar con actor inválido", func(t *testing.T) {
		if _, err := confirmed(t).HandleCommand(Cancel{By: CancelledBy("otro"), Now: now}); err == nil {
			t.Fatal("actor inválido debe fallar")
		}
	})
	t.Run("la clínica cancela sin motivo", func(t *testing.T) {
		if _, err := confirmed(t).HandleCommand(Cancel{By: ByClinic, Now: now}); err == nil {
			t.Fatal("la clínica debe indicar motivo")
		}
	})
	t.Run("cancelar una cita ya cancelada", func(t *testing.T) {
		b := confirmed(t)
		mustHandle(t, b, Cancel{By: ByClinic, Reason: "Cierre", ByUser: recepID, Now: now})
		_, err := b.HandleCommand(Cancel{By: ByClinic, Reason: "otra", Now: now})
		wantCode(t, err, "INVALID_TRANSITION")
	})
	t.Run("reprogramar sin estar confirmada", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(Reschedule{Start: slotStart, End: slotEnd, ProfessionalOK: true, Now: now})
		wantCode(t, err, "INVALID_TRANSITION")
	})
	t.Run("reprogramar a una franja pasada", func(t *testing.T) {
		_, err := confirmed(t).HandleCommand(Reschedule{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), ProfessionalOK: true, Now: now})
		if err == nil {
			t.Fatal("franja pasada debe fallar")
		}
	})
	t.Run("reprogramar con profesional no válido", func(t *testing.T) {
		_, err := confirmed(t).HandleCommand(Reschedule{Start: slotStart.Add(time.Hour), End: slotEnd.Add(time.Hour), Now: now})
		wantCode(t, err, "INVALID_PROFESSIONAL")
	})
	t.Run("reprogramar con otro profesional", func(t *testing.T) {
		b := confirmed(t)
		other := uuid.MustParse("00000000-0000-7000-8000-0000000000e2")
		mustHandle(t, b, Reschedule{ProfessionalID: &other, Start: slotStart.Add(time.Hour), End: slotEnd.Add(time.Hour), TZ: "America/Bogota", ProfessionalOK: true, By: recepID, Now: now})
		if p := b.ProfessionalID(); p == nil || *p != other {
			t.Fatalf("profesional = %v", p)
		}
	})
	t.Run("confirmar una franja pasada", func(t *testing.T) {
		b := &Appointment{}
		mustHandle(t, b, requestCmd())
		_, err := b.HandleCommand(ConfirmAppointment{ProfessionalID: vetID, Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), ProfessionalOK: true, Now: now})
		if err == nil {
			t.Fatal("franja pasada debe fallar")
		}
	})
}

func TestAppointment_PreferenceValidation(t *testing.T) {
	min, max := now.Add(2*time.Hour), now.Add(60*24*time.Hour)
	from := mustCivil(t, "2026-11-10")
	to := mustCivil(t, "2026-11-12")
	tooFar := mustCivil(t, "2027-06-01")
	farTo := mustCivil(t, "2027-06-03")
	soon := now.Add(30 * time.Minute)
	soonEnd := soon.Add(30 * time.Minute)
	cases := []struct {
		name string
		p    Preference
		ok   bool
	}{
		{"franja válida", Preference{Start: &slotStart, End: &slotEnd}, true},
		{"rango de fechas válido", Preference{DateFrom: &from, DateTo: &to}, true},
		{"rango invertido", Preference{DateFrom: &to, DateTo: &from}, false},
		{"rango fuera de antelación", Preference{DateFrom: &tooFar, DateTo: &farTo}, false},
		{"preferencia vacía", Preference{}, false},
		{"mezcla de franja y fechas", Preference{Start: &slotStart, End: &slotEnd, DateFrom: &from, DateTo: &to}, false},
		{"franja invertida", Preference{Start: &slotEnd, End: &slotStart}, false},
		{"franja fuera de antelación", Preference{Start: &soon, End: &soonEnd}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPreference(tc.p, min, max)
			if (err == nil) != tc.ok {
				t.Fatalf("error = %v, ok esperado %v", err, tc.ok)
			}
		})
	}
}
