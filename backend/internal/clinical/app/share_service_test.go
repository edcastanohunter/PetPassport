package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type memShares struct {
	recs map[uuid.UUID]*app.ShareRecord
}

func newMemShares() *memShares { return &memShares{recs: map[uuid.UUID]*app.ShareRecord{}} }

func (m *memShares) Create(_ context.Context, s app.ShareRecord) error {
	active := 0
	for _, r := range m.recs {
		if r.PetID == s.PetID && r.RevokedAt == nil {
			active++
		}
	}
	if active >= app.MaxActiveShares {
		return kernel.NewDomainError("SHARE_LIMIT", "límite")
	}
	m.recs[s.ID] = &s
	return nil
}

func (m *memShares) List(_ context.Context, petID uuid.UUID) ([]app.ShareView, error) {
	var out []app.ShareView
	for _, r := range m.recs {
		if r.PetID == petID {
			out = append(out, r.ShareView)
		}
	}
	return out, nil
}

func (m *memShares) Revoke(_ context.Context, _, id uuid.UUID, at time.Time) error {
	m.recs[id].RevokedAt = &at
	return nil
}

func (m *memShares) ByTokenHash(_ context.Context, h []byte) (app.ShareRecord, error) {
	for _, r := range m.recs {
		if string(r.TokenHash) == string(h) {
			return *r, nil
		}
	}
	return app.ShareRecord{}, &kernel.NotFoundError{Resource: "share"}
}

func (m *memShares) RecordAccess(_ context.Context, id uuid.UUID, _ time.Time) error {
	m.recs[id].AccessCount++
	return nil
}

func (m *memShares) PinFailed(_ context.Context, id uuid.UUID, until time.Time, max int) (bool, error) {
	r := m.recs[id]
	r.FailedPins++
	if r.FailedPins >= max {
		r.LockedUntil = &until
		r.FailedPins = 0
		return true, nil
	}
	return false, nil
}

func (m *memShares) PinOK(_ context.Context, id uuid.UUID) error {
	m.recs[id].FailedPins = 0
	return nil
}

type stubCards struct{}

func (stubCards) Card(_ context.Context, id uuid.UUID, _ bool) (app.CardView, error) {
	return app.CardView{PetID: id, Species: "dog"}, nil
}

func (stubCards) Entry(context.Context, uuid.UUID, uuid.UUID) (app.CardEntryView, error) {
	return app.CardEntryView{}, nil
}

type stubPets struct{}

func (stubPets) PublicPet(context.Context, uuid.UUID) (app.PublicPet, error) {
	return app.PublicPet{Name: "Luna", Species: "dog", Sex: "female"}, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func setup() (*app.ShareService, *memShares, *fakeClock) {
	m := newMemShares()
	clk := &fakeClock{t: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	return app.NewShareService(m, stubCards{}, stubPets{}, clk, nil), m, clk
}

func TestShare_CreateValidates(t *testing.T) {
	svc, _, _ := setup()
	ctx := context.Background()
	pet, user := uuid.New(), uuid.New()
	for name, tc := range map[string]struct {
		scope string
		days  int
		pin   string
	}{
		"scope":    {"nope", 7, ""},
		"days-low": {"vaccination_card", 0, ""},
		"days-hi":  {"vaccination_card", 4000, ""},
		"pin":      {"vaccination_card", 7, "12"},
	} {
		if _, err := svc.Create(ctx, pet, user, tc.scope, tc.days, tc.pin); err == nil {
			t.Errorf("%s: se esperaba error de validación", name)
		}
	}
}

func TestShare_ViewWithoutPIN(t *testing.T) {
	svc, m, _ := setup()
	ctx := context.Background()
	pet := uuid.New()
	c, err := svc.Create(ctx, pet, uuid.New(), "vaccination_card", 7, "")
	if err != nil {
		t.Fatal(err)
	}
	v, id, err := svc.View(ctx, c.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Pet.Name != "Luna" || m.recs[id].AccessCount != 1 {
		t.Fatalf("vista/contador inesperados: %+v / %d", v.Pet, m.recs[id].AccessCount)
	}
	if _, _, err := svc.View(ctx, "token-desconocido-000000000000000", ""); err == nil {
		t.Fatal("un token desconocido debe fallar (404)")
	}
}

func TestShare_ExpiredAndRevoked(t *testing.T) {
	svc, _, clk := setup()
	ctx := context.Background()
	pet := uuid.New()
	c, _ := svc.Create(ctx, pet, uuid.New(), "vaccination_card", 1, "")
	clk.t = clk.t.Add(25 * time.Hour)
	if _, _, err := svc.View(ctx, c.Token, ""); !errors.Is(err, app.ErrShareExpired) {
		t.Fatalf("caducado: %v", err)
	}
	clk.t = clk.t.Add(-25 * time.Hour)
	if err := svc.Revoke(ctx, pet, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.View(ctx, c.Token, ""); !errors.Is(err, app.ErrShareExpired) {
		t.Fatalf("revocado: %v", err)
	}
}

func TestShare_PINFlowAndLock(t *testing.T) {
	svc, _, clk := setup()
	ctx := context.Background()
	c, err := svc.Create(ctx, uuid.New(), uuid.New(), "full_record", 7, "1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.View(ctx, c.Token, ""); !errors.Is(err, app.ErrPinRequired) {
		t.Fatalf("sin desbloqueo: %v", err)
	}
	if _, _, err := svc.Unlock(ctx, c.Token, "0000"); !errors.Is(err, app.ErrPinInvalid) {
		t.Fatalf("PIN incorrecto: %v", err)
	}
	tok, exp, err := svc.Unlock(ctx, c.Token, "1234")
	if err != nil || tok == "" {
		t.Fatalf("PIN correcto: %v", err)
	}
	if _, _, err := svc.View(ctx, c.Token, tok); err != nil {
		t.Fatalf("con token: %v", err)
	}
	if _, _, err := svc.View(ctx, c.Token, tok+"x"); !errors.Is(err, app.ErrPinRequired) {
		t.Fatalf("token alterado: %v", err)
	}
	clk.t = exp.Add(time.Second)
	if _, _, err := svc.View(ctx, c.Token, tok); !errors.Is(err, app.ErrPinRequired) {
		t.Fatalf("token vencido: %v", err)
	}
	// Cinco fallos bloquean una hora.
	var last error
	for range app.MaxPinAttempts {
		_, _, last = svc.Unlock(ctx, c.Token, "9999")
	}
	if !errors.Is(last, app.ErrShareLocked) {
		t.Fatalf("bloqueo tras %d fallos: %v", app.MaxPinAttempts, last)
	}
	if _, _, err := svc.Unlock(ctx, c.Token, "1234"); !errors.Is(err, app.ErrShareLocked) {
		t.Fatalf("bloqueado aunque el PIN sea correcto: %v", err)
	}
	clk.t = clk.t.Add(app.PinLockDuration + time.Minute)
	if _, _, err := svc.Unlock(ctx, c.Token, "1234"); err != nil {
		t.Fatalf("tras el bloqueo: %v", err)
	}
}

func TestCardFingerprint_StableAndActiveOnly(t *testing.T) {
	a := app.CardEntryView{ID: uuid.New(), Kind: "vaccine", ProductName: "Rabia", AdministeredAt: "2026-01-01", Verification: "verified", Status: "active"}
	b := app.CardEntryView{ID: uuid.New(), Kind: "vaccine", ProductName: "Otra", AdministeredAt: "2026-02-01", Verification: "unverified", Status: "active"}
	voided := app.CardEntryView{ID: uuid.New(), Kind: "vaccine", ProductName: "X", AdministeredAt: "2026-03-01", Status: "voided"}
	h1 := app.CardFingerprint([]app.CardEntryView{a, b})
	if h1 != app.CardFingerprint([]app.CardEntryView{b, a, voided}) {
		t.Fatal("la huella debe ser independiente del orden y de las entradas anuladas")
	}
	b.Verification = "verified"
	if h1 == app.CardFingerprint([]app.CardEntryView{a, b}) {
		t.Fatal("cambiar la verificación debe cambiar la huella")
	}
}
