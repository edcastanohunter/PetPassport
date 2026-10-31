package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
)

type fakeChannel struct {
	sent []app.Email
	err  error
}

func (c *fakeChannel) Send(_ context.Context, m app.Email) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	c.sent = append(c.sent, m)
	return "prov-1", nil
}

type fakeStores struct {
	prefs map[uuid.UUID]domain.Preferences
	supp  map[string]string
	last  map[string]time.Time
}

func (f *fakeStores) Preferences(_ context.Context, u uuid.UUID) (domain.Preferences, error) {
	if p, ok := f.prefs[u]; ok {
		return p, nil
	}
	return domain.DefaultPreferences(), nil
}
func (f *fakeStores) Suppressed(_ context.Context, h []byte) (bool, error) {
	_, ok := f.supp[string(h)]
	return ok, nil
}
func (f *fakeStores) Suppress(_ context.Context, h []byte, reason string) error {
	f.supp[string(h)] = reason
	return nil
}
func (f *fakeStores) LastSent(_ context.Context, r uuid.UUID, tpl string) (time.Time, bool, error) {
	t, ok := f.last[r.String()+"|"+tpl]
	return t, ok, nil
}

type senderEnv struct {
	s      *app.Sender
	ch     *fakeChannel
	st     *fakeStores
	dir    *fakeDir
	user   app.User
	now    time.Time
	secret []byte
}

func newSender(t *testing.T) *senderEnv {
	t.Helper()
	e := &senderEnv{
		ch: &fakeChannel{}, st: &fakeStores{prefs: map[uuid.UUID]domain.Preferences{}, supp: map[string]string{}, last: map[string]time.Time{}},
		dir: newDir(), now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), secret: []byte("secreto-de-prueba-0123456789abcdef"),
	}
	e.user = app.User{ID: uuid.New(), Email: "Tutor@Example.com", Locale: "en", Country: "CO"}
	e.dir.users[e.user.ID] = e.user
	e.s = app.NewSender(e.dir, e.st, e.st, e.st, e.ch, app.SenderConfig{
		BaseURL: "https://app.example", APIBaseURL: "https://app.example/api/v1", Address: "PetPassport · Bogotá",
		FromDomain: "notify.example", UnsubscribeSecret: e.secret,
	}, fixedClock{e.now})
	return e
}

func (e *senderEnv) delivery(tpl string, cat domain.Category, vars map[string]string) app.Delivery {
	return app.Delivery{ID: uuid.New(), Category: cat, Template: tpl, Locale: "es", RecipientID: e.user.ID, Payload: vars, Attempts: 1}
}

func apptPayload() map[string]string {
	return map[string]string{
		"pet_name": "Luna", "org_name": "Clínica Sur", "when@time": "2026-11-02T14:00:00Z", "tz": "America/Bogota",
		"link_path": "/app/appointments",
	}
}

func TestDeliverSendsLocalizedEmail(t *testing.T) {
	e := newSender(t)
	out, err := e.s.Deliver(context.Background(), e.delivery("appointment_confirmed", domain.Appointments, apptPayload()))
	if err != nil || out.Status != domain.StatusSent || out.MessageID != "prov-1" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	m := e.ch.sent[0]
	if m.To != "Tutor@Example.com" {
		t.Fatalf("to: %q", m.To)
	}
	// Idioma de la cuenta (en), hora en la zona de la clínica (14:00Z = 09:00 Bogotá).
	if !strings.Contains(m.Subject, "Appointment confirmed") || !strings.Contains(m.Subject, "Mon, Nov 2 · 9:00 AM") {
		t.Fatalf("subject: %q", m.Subject)
	}
	if !strings.Contains(m.HTML, "https://app.example/app/appointments") || !strings.Contains(m.Text, "/unsubscribe/") {
		t.Fatalf("enlaces: %s", m.Text)
	}
	if !strings.HasPrefix(m.MessageID, "<") || !strings.HasSuffix(m.MessageID, "@notify.example>") {
		t.Fatalf("message-id: %q", m.MessageID)
	}
	if !strings.HasPrefix(m.UnsubscribeURL, "https://app.example/api/v1/unsubscribe/") {
		t.Fatalf("baja de un clic: %q", m.UnsubscribeURL)
	}
	// El token del enlace verifica y apunta a la categoría de la entrega.
	tok := m.UnsubscribeURL[strings.LastIndex(m.UnsubscribeURL, "/")+1:]
	u, c, err := domain.VerifyUnsubscribe(e.secret, tok, e.now)
	if err != nil || u != e.user.ID || c != domain.Appointments {
		t.Fatalf("token: %v %v %v", u, c, err)
	}
}

func TestDeliverSecurityHasNoUnsubscribe(t *testing.T) {
	e := newSender(t)
	d := e.delivery("transfer_done", domain.Security, map[string]string{"pet_name": "Luna", "to_name": "X", "link_path": "/app"})
	out, _ := e.s.Deliver(context.Background(), d)
	if out.Status != domain.StatusSent {
		t.Fatalf("out=%+v", out)
	}
	if e.ch.sent[0].UnsubscribeURL != "" || strings.Contains(e.ch.sent[0].Text, "/unsubscribe/") {
		t.Fatal("security no ofrece baja")
	}
}

func TestDeliverSuppressedByPreference(t *testing.T) {
	e := newSender(t)
	p := domain.DefaultPreferences()
	p.Email[domain.Appointments] = false
	e.st.prefs[e.user.ID] = p
	out, _ := e.s.Deliver(context.Background(), e.delivery("appointment_confirmed", domain.Appointments, apptPayload()))
	if out.Status != domain.StatusSuppressed || out.Reason != "preference" || len(e.ch.sent) != 0 {
		t.Fatalf("out=%+v sent=%d", out, len(e.ch.sent))
	}
	// La preferencia se consulta al enviar: otra categoría sigue pasando.
	out, _ = e.s.Deliver(context.Background(), e.delivery("encounter_signed", domain.Clinical,
		map[string]string{"pet_name": "Luna", "org_name": "Clínica Sur", "link_path": "/app"}))
	if out.Status != domain.StatusSent {
		t.Fatalf("out=%+v", out)
	}
}

func TestDeliverSuppressedAddress(t *testing.T) {
	e := newSender(t)
	e.st.supp[string(app.AddressHash("tutor@example.com"))] = "bounce"
	out, _ := e.s.Deliver(context.Background(), e.delivery("appointment_confirmed", domain.Appointments, apptPayload()))
	if out.Status != domain.StatusSuppressed || out.Reason != "address" || len(e.ch.sent) != 0 {
		t.Fatalf("out=%+v", out)
	}
}

func TestDeliverThrottleDefersButNotReminders(t *testing.T) {
	e := newSender(t)
	e.st.last[e.user.ID.String()+"|appointment_confirmed"] = e.now.Add(-20 * time.Minute)
	e.st.last[e.user.ID.String()+"|appointment_reminder"] = e.now.Add(-20 * time.Minute)
	out, _ := e.s.Deliver(context.Background(), e.delivery("appointment_confirmed", domain.Appointments, apptPayload()))
	if out.Status != domain.StatusQueued || !out.Deferred || out.Wait != 40*time.Minute || len(e.ch.sent) != 0 {
		t.Fatalf("la limitación difiere 40 min: %+v", out)
	}
	out, _ = e.s.Deliver(context.Background(), e.delivery("appointment_reminder", domain.Appointments, apptPayload()))
	if out.Status != domain.StatusSent {
		t.Fatalf("los recordatorios de cita no se limitan: %+v", out)
	}
}

func TestDeliverFailureClassification(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		attempts int
		status   string
		dead     bool
		wait     time.Duration
		suppress bool
	}{
		{"transitorio reintenta con backoff", errors.New("timeout"), 1, domain.StatusFailed, false, 30 * time.Second, false},
		{"transitorio en el tercer intento", errors.New("timeout"), 3, domain.StatusFailed, false, 10 * time.Minute, false},
		{"quinto intento es dead-letter", errors.New("timeout"), 5, domain.StatusFailed, true, 0, false},
		{"permanente no reintenta", app.ErrPermanent, 1, domain.StatusFailed, true, 0, false},
		{"dirección inválida suprime", app.ErrInvalidAddress, 1, domain.StatusFailed, true, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newSender(t)
			e.ch.err = c.err
			d := e.delivery("appointment_confirmed", domain.Appointments, apptPayload())
			d.Attempts = c.attempts
			out, err := e.s.Deliver(context.Background(), d)
			if err != nil || out.Status != c.status || out.Dead != c.dead || out.Wait != c.wait {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			if _, ok := e.st.supp[string(app.AddressHash("tutor@example.com"))]; ok != c.suppress {
				t.Fatalf("supresión = %v, esperaba %v", ok, c.suppress)
			}
		})
	}
}

func TestDeliverGhostRecipientUsesPayloadAddress(t *testing.T) {
	e := newSender(t)
	ghost := app.Ghost("contacto@sur.co")
	d := app.Delivery{
		ID: uuid.New(), Category: domain.Account, Template: "org_status", Locale: "es", RecipientID: ghost.ID, Attempts: 1,
		Payload: map[string]string{app.KeyTo: "contacto@sur.co", "org_name": "Clínica Sur", "status": "verified", "reason": "", "link_path": "/app"},
	}
	out, _ := e.s.Deliver(context.Background(), d)
	if out.Status != domain.StatusSent || e.ch.sent[0].To != "contacto@sur.co" {
		t.Fatalf("out=%+v sent=%+v", out, e.ch.sent)
	}
	if e.ch.sent[0].UnsubscribeURL != "" {
		t.Fatal("sin cuenta no hay baja por token")
	}
}

func TestDeliverUnknownRecipientAndBrokenTemplateFailPermanently(t *testing.T) {
	e := newSender(t)
	d := e.delivery("appointment_confirmed", domain.Appointments, apptPayload())
	d.RecipientID = uuid.New() // cuenta inexistente
	if out, _ := e.s.Deliver(context.Background(), d); out.Status != domain.StatusFailed || !out.Dead {
		t.Fatalf("out=%+v", out)
	}
	d = e.delivery("appointment_confirmed", domain.Appointments, map[string]string{"pet_name": "Luna", "link_path": "/app"})
	if out, _ := e.s.Deliver(context.Background(), d); out.Status != domain.StatusFailed || !out.Dead || out.Reason != "render" {
		t.Fatalf("faltan variables → fallo permanente: %+v", out)
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		" Tutor@Example.COM ":   "tutor@example.com",
		"Ana <Ana@Example.com>": "ana@example.com",
		"no es una dirección":   "no es una dirección",
		"UPPER@EXAMPLE.COM":     "upper@example.com",
	} {
		if got := app.NormalizeAddress(in); got != want {
			t.Errorf("%q → %q, esperaba %q", in, got, want)
		}
	}
	if string(app.AddressHash("A@b.co")) != string(app.AddressHash(" a@B.CO ")) {
		t.Fatal("el hash ignora mayúsculas y espacios")
	}
}

// ---- preferencias y baja

type memPrefs struct {
	m map[uuid.UUID]domain.Preferences
}

func (s *memPrefs) Get(_ context.Context, u uuid.UUID) (domain.Preferences, bool, error) {
	p, ok := s.m[u]
	return p, ok, nil
}
func (s *memPrefs) Put(_ context.Context, u uuid.UUID, p domain.Preferences) error {
	s.m[u] = p
	return nil
}

func TestPreferencesService(t *testing.T) {
	secret := []byte("secreto-de-prueba-0123456789abcdef")
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store := &memPrefs{m: map[uuid.UUID]domain.Preferences{}}
	svc := app.NewPreferences(store, secret, fixedClock{now})
	user := uuid.New()
	ctx := context.Background()

	p, err := svc.Get(ctx, user)
	if err != nil || !p.Allows(domain.Appointments) {
		t.Fatalf("defaults: %+v %v", p, err)
	}
	p.Email[domain.Security] = false // se ignora
	p.Email[domain.Clinical] = false
	got, err := svc.Put(ctx, user, p)
	if err != nil || !got.Allows(domain.Security) || got.Allows(domain.Clinical) {
		t.Fatalf("put: %+v %v", got, err)
	}

	tok := domain.SignUnsubscribe(secret, user, domain.Appointments, now.Add(time.Hour))
	cat, err := svc.Unsubscribe(ctx, tok)
	if err != nil || cat != domain.Appointments {
		t.Fatalf("unsubscribe: %v %v", cat, err)
	}
	if p, _ := svc.Get(ctx, user); p.Allows(domain.Appointments) || p.Allows(domain.Clinical) {
		t.Fatalf("la baja conserva lo demás: %+v", p)
	}
	if _, err := svc.Unsubscribe(ctx, tok); err != nil { // idempotente
		t.Fatalf("repetir la baja no falla: %v", err)
	}
	if _, err := svc.Unsubscribe(ctx, "basura"); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("err: %v", err)
	}
}

// ---- validación al despachar

func TestStillValid(t *testing.T) {
	w := newWorld()
	ctx := context.Background()
	appt, entry, enc := uuid.New(), uuid.New(), uuid.New()
	w.dir.appts[appt] = app.Appointment{ID: appt, Status: "confirmed", GuardianID: w.guardian}
	w.dir.entries[entry] = app.CardEntry{ID: entry, PetID: w.pet, Status: "active", NextDue: "2026-12-01"}
	w.dir.encs[enc] = app.Encounter{ID: enc, PetID: w.pet, FollowUp: &app.FollowUp{DueDate: "2026-11-20"}}

	cases := []struct {
		name string
		s    app.Scheduled
		want bool
	}{
		{"cita confirmada", app.Scheduled{SubjectType: app.SubjectAppointment, SubjectID: appt, RecipientID: w.guardian}, true},
		{"otro destinatario", app.Scheduled{SubjectType: app.SubjectAppointment, SubjectID: appt, RecipientID: uuid.New()}, false},
		{"cita inexistente", app.Scheduled{SubjectType: app.SubjectAppointment, SubjectID: uuid.New(), RecipientID: w.guardian}, false},
		{"dosis vigente", app.Scheduled{SubjectType: app.SubjectCardEntry, SubjectID: entry, RecipientID: w.guardian, Payload: map[string]string{"due_date": "2026-12-01"}}, true},
		{"dosis con otra fecha", app.Scheduled{SubjectType: app.SubjectCardEntry, SubjectID: entry, RecipientID: w.guardian, Payload: map[string]string{"due_date": "2026-11-01"}}, false},
		{"control vigente", app.Scheduled{SubjectType: app.SubjectEncounter, SubjectID: enc, RecipientID: w.guardian, Payload: map[string]string{"due_date": "2026-11-20"}}, true},
		{"sujeto desconocido", app.Scheduled{SubjectType: "otro", SubjectID: enc, RecipientID: w.guardian}, false},
	}
	for _, c := range cases {
		if got, err := app.StillValid(ctx, w.dir, c.s); err != nil || got != c.want {
			t.Errorf("%s: %v %v, esperaba %v", c.name, got, err, c.want)
		}
	}

	w.dir.appts[appt] = app.Appointment{ID: appt, Status: "cancelled", GuardianID: w.guardian}
	if ok, _ := app.StillValid(ctx, w.dir, cases[0].s); ok {
		t.Error("una cita cancelada ya no recibe recordatorio")
	}
	w.dir.entries[entry] = app.CardEntry{ID: entry, PetID: w.pet, Status: "voided", NextDue: "2026-12-01"}
	if ok, _ := app.StillValid(ctx, w.dir, cases[3].s); ok {
		t.Error("una entrada anulada no recuerda")
	}
	w.dir.entries[entry] = app.CardEntry{ID: entry, PetID: w.pet, Status: "active", NextDue: "2026-12-01"}
	w.dir.pets[w.pet] = app.Pet{ID: w.pet, GuardianID: w.guardian, Deceased: true}
	if ok, _ := app.StillValid(ctx, w.dir, cases[3].s); ok {
		t.Error("una mascota fallecida no recibe recordatorios")
	}
	w.dir.pets[w.pet] = app.Pet{ID: w.pet, GuardianID: uuid.New()}
	if ok, _ := app.StillValid(ctx, w.dir, cases[3].s); ok {
		t.Error("tras una transferencia el antiguo tutor deja de recibirlos")
	}
}
