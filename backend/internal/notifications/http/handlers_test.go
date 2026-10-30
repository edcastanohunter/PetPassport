package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

type fakeSvc struct {
	prefs domain.Preferences
	token error
	cat   domain.Category
}

func (f *fakeSvc) Get(context.Context, uuid.UUID) (domain.Preferences, error) { return f.prefs, nil }
func (f *fakeSvc) Put(_ context.Context, _ uuid.UUID, p domain.Preferences) (domain.Preferences, error) {
	f.prefs = p
	return p, nil
}
func (f *fakeSvc) Unsubscribe(context.Context, string) (domain.Category, error) {
	return f.cat, f.token
}

type fakeSupp struct{ got []string }

func (f *fakeSupp) Suppress(_ context.Context, _ []byte, reason string) error {
	f.got = append(f.got, reason)
	return nil
}

func sign(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func authed() context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{
		UserID: uuid.New(), EmailVerified: true, GlobalRoles: []string{"pet_owner"},
		Account: auth.AccountState{OnboardingCompleted: true, ConsentCurrent: true},
	})
}

func TestValidSignature(t *testing.T) {
	secret, body := []byte("s3cret"), []byte(`{"events":[]}`)
	good := sign(secret, body)
	cases := []struct {
		name   string
		secret []byte
		body   []byte
		header string
		want   bool
	}{
		{"firma correcta", secret, body, good, true},
		{"secreto distinto", []byte("otro"), body, good, false},
		{"cuerpo alterado", secret, []byte(`{"events":[1]}`), good, false},
		{"sin prefijo", secret, body, good[len("sha256="):], false},
		{"hex inválido", secret, body, "sha256=zz", false},
		{"cuerpo vacío", secret, nil, sign(secret, nil), false},
		{"cabecera vacía", secret, body, "", false},
	}
	for _, c := range cases {
		if got := validSignature(c.secret, c.body, c.header); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPreferences_DefaultsAndSecurityAlwaysOn(t *testing.T) {
	svc := &fakeSvc{}
	h := NewNotificationHandlers(svc, nil, nil, nil)
	body := gen.NotificationPreferences{}
	body.Email.Security = false // el cliente intenta apagarla
	body.Email.Clinical = true
	out, err := h.PutNotificationPreferences(authed(), gen.PutNotificationPreferencesRequestObject{Body: (*gen.PutNotificationPreferencesJSONRequestBody)(&body)})
	if err != nil {
		t.Fatal(err)
	}
	got := gen.NotificationPreferences(out.(gen.PutNotificationPreferences200JSONResponse))
	if !got.Email.Security {
		t.Fatal("security no puede apagarse (NOT-004)")
	}
	if !svc.prefs.Allows(domain.Clinical) || svc.prefs.Allows(domain.Appointments) {
		t.Fatalf("preferencias guardadas: %+v", svc.prefs)
	}
}

func TestPreferences_RequireAuthAndBody(t *testing.T) {
	h := NewNotificationHandlers(&fakeSvc{}, nil, nil, nil)
	if _, err := h.GetNotificationPreferences(context.Background(), gen.GetNotificationPreferencesRequestObject{}); problem.From(err).Status != http.StatusForbidden {
		t.Fatalf("sin sesión: %v", err)
	}
	if _, err := h.PutNotificationPreferences(authed(), gen.PutNotificationPreferencesRequestObject{}); problem.From(err).Status != http.StatusUnprocessableEntity {
		t.Fatalf("sin cuerpo: %v", err)
	}
}

func TestUnsubscribe_InvalidTokenIs404(t *testing.T) {
	h := NewNotificationHandlers(&fakeSvc{token: domain.ErrTokenInvalid}, nil, nil, nil)
	_, err := h.UnsubscribeNotifications(context.Background(), gen.UnsubscribeNotificationsRequestObject{Token: "x"})
	if p := problem.From(err); p.Status != http.StatusNotFound {
		t.Fatalf("problem: %+v", p)
	}
	h = NewNotificationHandlers(&fakeSvc{cat: domain.Appointments}, nil, nil, nil)
	out, err := h.UnsubscribeNotifications(context.Background(), gen.UnsubscribeNotificationsRequestObject{Token: "x"})
	if err != nil || out.(gen.UnsubscribeNotifications200JSONResponse).Category != "appointments" {
		t.Fatalf("baja: %v %v", out, err)
	}
}

func TestWebhook_UnknownProviderIs404_AndBadSignatureIs401(t *testing.T) {
	supp := &fakeSupp{}
	h := NewNotificationHandlers(&fakeSvc{}, supp, map[string][]byte{"generic": []byte("s3cret")}, nil)
	req := gen.EmailProviderWebhookRequestObject{Provider: "otro", Body: &gen.EmailProviderWebhookJSONRequestBody{}}
	if _, err := h.EmailProviderWebhook(context.Background(), req); problem.From(err).Status != http.StatusNotFound {
		t.Fatalf("proveedor desconocido: %v", err)
	}
	req.Provider = "generic"
	req.Params.XPPSignature = "sha256=00"
	p := problem.From(errOf(h.EmailProviderWebhook(context.Background(), req)))
	if p.Status != http.StatusUnauthorized || p.Code != "WEBHOOK_SIGNATURE_INVALID" {
		t.Fatalf("firma inválida: %+v", p)
	}
	if len(supp.got) != 0 {
		t.Fatal("con firma inválida no se suprime nada")
	}
}

func TestWebhook_NoSecretsConfiguredIs404(t *testing.T) {
	h := NewNotificationHandlers(&fakeSvc{}, &fakeSupp{}, nil, nil)
	req := gen.EmailProviderWebhookRequestObject{Provider: "generic"}
	if _, err := h.EmailProviderWebhook(context.Background(), req); problem.From(err).Status != http.StatusNotFound {
		t.Fatalf("sin proveedor configurado: %v", err)
	}
}

func errOf(_ gen.EmailProviderWebhookResponseObject, err error) error { return err }
