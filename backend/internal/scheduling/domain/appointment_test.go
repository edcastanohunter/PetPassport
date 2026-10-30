package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	apptID    = uuid.MustParse("00000000-0000-7000-8000-0000000000a1")
	petID     = uuid.MustParse("00000000-0000-7000-8000-0000000000b1")
	guardID   = uuid.MustParse("00000000-0000-7000-8000-0000000000c1")
	orgID     = uuid.MustParse("00000000-0000-7000-8000-0000000000d1")
	vetID     = uuid.MustParse("00000000-0000-7000-8000-0000000000e1")
	recepID   = uuid.MustParse("00000000-0000-7000-8000-0000000000f1")
	now       = time.Date(2026, 11, 2, 13, 0, 0, 0, time.UTC)
	slotStart = time.Date(2026, 11, 4, 15, 0, 0, 0, time.UTC)
	slotEnd   = slotStart.Add(30 * time.Minute)
)

func requestCmd() RequestAppointment {
	return RequestAppointment{
		AppointmentID: apptID, PetID: petID, GuardianID: guardID, OrganizationID: orgID,
		Type: "consultation", Reason: "Cojera", Linked: true, Pending: 0,
		Preferences: []Preference{{Start: &slotStart, End: &slotEnd}},
		LeadMin:     2 * time.Hour, LeadMax: 60 * 24 * time.Hour, RequestedBy: guardID, Now: now,
	}
}

func mustHandle(t *testing.T, a *Appointment, cmd any) []any {
	t.Helper()
	evs, err := a.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("%T: %v", cmd, err)
	}
	return evs
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var de *kernel.DomainError
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("error = %v, want código %s", err, code)
	}
}

func confirmed(t *testing.T) *Appointment {
	t.Helper()
	a := &Appointment{}
	mustHandle(t, a, requestCmd())
	mustHandle(t, a, ConfirmAppointment{ProfessionalID: vetID, Start: slotStart, End: slotEnd, TZ: "America/Bogota", PolicyHours: 4, ProfessionalOK: true, By: recepID, Now: now})
	return a
}

func TestAppointment_RequestRules(t *testing.T) {
	t.Run("ok emite AppointmentRequested", func(t *testing.T) {
		a := &Appointment{}
		evs := mustHandle(t, a, requestCmd())
		if _, ok := evs[0].(AppointmentRequested); !ok || a.Status() != StatusRequested || a.Version() != 1 {
			t.Fatalf("eventos=%v status=%s v=%d", evs, a.Status(), a.Version())
		}
	})
	t.Run("A1 sin vínculo", func(t *testing.T) {
		c := requestCmd()
		c.Linked = false
		_, err := (&Appointment{}).HandleCommand(c)
		wantCode(t, err, "PET_NOT_ACCESSIBLE")
	})
	t.Run("A2 límite de solicitudes", func(t *testing.T) {
		c := requestCmd()
		c.Pending = MaxPendingRequests
		_, err := (&Appointment{}).HandleCommand(c)
		wantCode(t, err, "REQUEST_LIMIT")
	})
	t.Run("antelación mínima", func(t *testing.T) {
		c := requestCmd()
		soon, soonEnd := now.Add(30*time.Minute), now.Add(time.Hour)
		c.Preferences = []Preference{{Start: &soon, End: &soonEnd}}
		_, err := (&Appointment{}).HandleCommand(c)
		wantCode(t, err, "OUTSIDE_LEAD_TIME")
	})
	t.Run("antelación máxima", func(t *testing.T) {
		c := requestCmd()
		far, farEnd := now.Add(90*24*time.Hour), now.Add(90*24*time.Hour+time.Hour)
		c.Preferences = []Preference{{Start: &far, End: &farEnd}}
		_, err := (&Appointment{}).HandleCommand(c)
		wantCode(t, err, "OUTSIDE_LEAD_TIME")
	})
	t.Run("rango de fechas", func(t *testing.T) {
		c := requestCmd()
		from, to := kernel.CivilDate("2026-11-05"), kernel.CivilDate("2026-11-09")
		c.Preferences = []Preference{{DateFrom: &from, DateTo: &to}}
		mustHandle(t, &Appointment{}, c)
	})
	t.Run("validaciones", func(t *testing.T) {
		for name, mut := range map[string]func(*RequestAppointment){
			"tipo":     func(c *RequestAppointment) { c.Type = "x" },
			"motivo":   func(c *RequestAppointment) { c.Reason = "  " },
			"sin pref": func(c *RequestAppointment) { c.Preferences = nil },
			"4 pref": func(c *RequestAppointment) {
				c.Preferences = append(c.Preferences, c.Preferences[0], c.Preferences[0], c.Preferences[0])
			},
			"slot al rev": func(c *RequestAppointment) { c.Preferences = []Preference{{Start: &slotEnd, End: &slotStart}} },
		} {
			c := requestCmd()
			mut(&c)
			_, err := (&Appointment{}).HandleCommand(c)
			var ve *kernel.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: se esperaba ValidationError, got %v", name, err)
			}
		}
	})
}

func TestAppointment_CreateDirect(t *testing.T) {
	c := CreateAppointment{
		AppointmentID: apptID, PetID: petID, GuardianID: guardID, OrganizationID: orgID, ProfessionalID: vetID,
		Type: "vaccination", Reason: "Refuerzo", Start: slotStart, End: slotEnd, TZ: "America/Bogota", PolicyHours: 4,
		Linked: true, ProfessionalOK: true, CreatedBy: recepID, Now: now,
	}
	a := &Appointment{}
	evs := mustHandle(t, a, c)
	if len(evs) != 2 || a.Status() != StatusConfirmed || a.Origin() != OriginClinic {
		t.Fatalf("eventos=%d status=%s", len(evs), a.Status())
	}
	c.ProfessionalOK = false
	_, err := (&Appointment{}).HandleCommand(c)
	wantCode(t, err, "INVALID_PROFESSIONAL")
	c.ProfessionalOK, c.Linked = true, false
	_, err = (&Appointment{}).HandleCommand(c)
	wantCode(t, err, "PET_NOT_ACCESSIBLE")
	c.Linked, c.End = true, c.Start.Add(time.Minute) // A7: < 5 min
	if _, err = (&Appointment{}).HandleCommand(c); err == nil {
		t.Fatal("A7: duración < 5 min debe fallar")
	}
}

func TestAppointment_ProposeAcceptExpire(t *testing.T) {
	a := &Appointment{}
	mustHandle(t, a, requestCmd())
	alt, altEnd := slotStart.Add(24*time.Hour), slotEnd.Add(24*time.Hour)
	mustHandle(t, a, ProposeAlternative{ProfessionalID: vetID, Start: alt, End: altEnd, TZ: "America/Bogota", PolicyHours: 4, ProfessionalOK: true, By: recepID, Now: now})
	if a.Status() != StatusProposed || a.ProposalExpiresAt() == nil || !a.ProposalExpiresAt().Equal(now.Add(ProposalTTL)) {
		t.Fatalf("propuesta mal: %s %v", a.Status(), a.ProposalExpiresAt())
	}
	t.Run("aceptar a tiempo confirma la franja propuesta", func(t *testing.T) {
		b := cloneByReplay(t, a)
		mustHandle(t, b, AcceptProposal{By: guardID, Now: now.Add(47 * time.Hour)})
		if b.Status() != StatusConfirmed || !b.Slot().Start.Equal(alt) {
			t.Fatalf("status=%s slot=%v", b.Status(), b.Slot())
		}
	})
	t.Run("aceptar caducada → PROPOSAL_EXPIRED", func(t *testing.T) {
		_, err := cloneByReplay(t, a).HandleCommand(AcceptProposal{By: guardID, Now: now.Add(49 * time.Hour)})
		wantCode(t, err, "PROPOSAL_EXPIRED")
	})
	t.Run("expirar es idempotente y respeta el plazo", func(t *testing.T) {
		b := cloneByReplay(t, a)
		if evs := mustHandle(t, b, ExpireProposal{Now: now.Add(time.Hour)}); len(evs) != 0 {
			t.Fatal("antes del plazo no hace nada")
		}
		if evs := mustHandle(t, b, ExpireProposal{Now: now.Add(49 * time.Hour)}); len(evs) != 1 || b.Status() != StatusRejected {
			t.Fatalf("expiró mal: %v %s", evs, b.Status())
		}
		if evs := mustHandle(t, b, ExpireProposal{Now: now.Add(50 * time.Hour)}); len(evs) != 0 {
			t.Fatal("segunda expiración no debe emitir")
		}
	})
}

// cloneByReplay reconstruye la cita desde sus eventos (como el repositorio).
func cloneByReplay(t *testing.T, a *Appointment) *Appointment {
	t.Helper()
	b := &Appointment{}
	// Los eventos del agregado original están en uncommitted (no se persistió).
	for _, e := range a.UncommittedEvents() {
		if err := b.ApplyEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestAppointment_Transitions(t *testing.T) {
	type step struct {
		name string
		cmd  any
		want Status
	}
	t.Run("confirmada → check-in → completada", func(t *testing.T) {
		a := confirmed(t)
		mustHandle(t, a, CheckIn{By: recepID, Now: slotStart.Add(-time.Hour)})
		enc := uuid.New()
		mustHandle(t, a, Complete{EncounterID: &enc, Now: slotStart.Add(time.Hour)})
		if a.Status() != StatusCompleted || a.EncounterID() == nil {
			t.Fatalf("status=%s", a.Status())
		}
		// Idempotente (reentrega del process manager).
		if evs := mustHandle(t, a, Complete{EncounterID: &enc, Now: slotStart.Add(2 * time.Hour)}); len(evs) != 0 {
			t.Fatal("completar dos veces no debe emitir")
		}
	})
	t.Run("A6 ventana de check-in", func(t *testing.T) {
		a := confirmed(t)
		_, err := a.HandleCommand(CheckIn{Now: slotStart.Add(-3 * time.Hour)})
		wantCode(t, err, "CHECKIN_WINDOW")
		_, err = a.HandleCommand(CheckIn{Now: slotStart.Add(26 * time.Hour)})
		wantCode(t, err, "CHECKIN_WINDOW")
	})
	t.Run("A8 transiciones inválidas", func(t *testing.T) {
		for _, s := range []step{
			{"check-in sin confirmar", CheckIn{Now: slotStart}, ""},
			{"no-show sin confirmar", MarkNoShow{Now: slotEnd.Add(time.Hour)}, ""},
			{"reprogramar sin confirmar", Reschedule{Start: slotStart, End: slotEnd, ProfessionalOK: true, Now: now}, ""},
			{"completar sin check-in ni consulta", Complete{Now: now}, ""},
		} {
			a := &Appointment{}
			mustHandle(t, a, requestCmd())
			_, err := a.HandleCommand(s.cmd)
			wantCode(t, err, "INVALID_TRANSITION")
		}
		a := confirmed(t)
		mustHandle(t, a, Cancel{By: ByClinic, Reason: "cierre", Now: now})
		for _, cmd := range []any{
			CheckIn{Now: slotStart}, Cancel{By: ByClinic, Reason: "x", Now: now},
			ConfirmAppointment{ProfessionalOK: true, Start: slotStart, End: slotEnd, Now: now},
			RejectRequest{Reason: "x", Now: now},
		} {
			_, err := a.HandleCommand(cmd)
			wantCode(t, err, "INVALID_TRANSITION")
		}
	})
	t.Run("reprogramar conserva el profesional y recuerda la franja previa", func(t *testing.T) {
		a := confirmed(t)
		ns, ne := slotStart.Add(48*time.Hour), slotEnd.Add(48*time.Hour)
		evs := mustHandle(t, a, Reschedule{Start: ns, End: ne, TZ: "America/Bogota", ProfessionalOK: true, By: recepID, Now: now})
		rs := evs[0].(AppointmentRescheduled)
		if !rs.PreviousSlot.Start.Equal(slotStart) || *a.ProfessionalID() != vetID || !a.Slot().Start.Equal(ns) {
			t.Fatalf("reprogramación mal: %+v", rs)
		}
	})
}

func TestAppointment_CancelLate(t *testing.T) {
	for name, tc := range map[string]struct {
		by   CancelledBy
		at   time.Time
		late bool
	}{
		"tutor con margen":      {ByGuardian, slotStart.Add(-5 * time.Hour), false},
		"tutor dentro política": {ByGuardian, slotStart.Add(-3 * time.Hour), true},
		"clínica nunca tardía":  {ByClinic, slotStart.Add(-time.Hour), false},
	} {
		a := confirmed(t)
		evs := mustHandle(t, a, Cancel{By: tc.by, Reason: "motivo", ByUser: guardID, Now: tc.at})
		if got := evs[0].(AppointmentCancelled).Late; got != tc.late {
			t.Errorf("%s: late=%v, want %v", name, got, tc.late)
		}
	}
	// Solicitud (sin franja): nunca es tardía.
	a := &Appointment{}
	mustHandle(t, a, requestCmd())
	evs := mustHandle(t, a, Cancel{By: ByGuardian, ByUser: guardID, Now: now})
	if evs[0].(AppointmentCancelled).Late {
		t.Error("cancelar una solicitud no es tardío")
	}
	// La clínica debe dar motivo.
	b := confirmed(t)
	if _, err := b.HandleCommand(Cancel{By: ByClinic, Now: now}); err == nil {
		t.Error("la clínica debe indicar motivo")
	}
}

func TestAppointment_NoShow(t *testing.T) {
	a := confirmed(t)
	if _, err := a.HandleCommand(MarkNoShow{Now: slotEnd.Add(-time.Minute)}); err == nil {
		t.Fatal("no-show antes del fin debe fallar")
	}
	b := confirmed(t)
	if evs := mustHandle(t, b, MarkNoShow{Automatic: true, Now: slotEnd.Add(time.Hour)}); len(evs) != 0 {
		t.Fatal("automático antes de 24 h no hace nada")
	}
	if evs := mustHandle(t, b, MarkNoShow{Automatic: true, Now: slotEnd.Add(25 * time.Hour)}); len(evs) != 1 || b.Status() != StatusNoShow {
		t.Fatalf("automático a las 24 h: %v %s", evs, b.Status())
	}
	if evs := mustHandle(t, b, MarkNoShow{Automatic: true, Now: slotEnd.Add(30 * time.Hour)}); len(evs) != 0 {
		t.Fatal("el automático es idempotente")
	}
	c := confirmed(t)
	mustHandle(t, c, MarkNoShow{Now: slotEnd.Add(time.Minute)})
	if c.Status() != StatusNoShow {
		t.Fatal("manual tras el fin")
	}
}

func TestAppointment_CompleteByReaction(t *testing.T) {
	a := confirmed(t)
	enc := uuid.New()
	mustHandle(t, a, Complete{EncounterID: &enc, Now: slotStart.Add(time.Hour)})
	if a.Status() != StatusCompleted {
		t.Fatalf("status=%s", a.Status())
	}
}

func TestAppointment_ReplayEqualsLive(t *testing.T) {
	a := confirmed(t)
	mustHandle(t, a, CheckIn{By: recepID, Now: slotStart.Add(-30 * time.Minute)})
	b := cloneByReplay(t, a)
	if b.Version() != a.Version() || b.Status() != a.Status() || len(b.History()) != len(a.History()) {
		t.Fatalf("replay distinto: %d/%s vs %d/%s", b.Version(), b.Status(), a.Version(), a.Status())
	}
}
