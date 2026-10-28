package keycloakadmin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
)

// fakeKeycloak imita los endpoints de IAM-011 y registra las llamadas.
type fakeKeycloak struct {
	mu          sync.Mutex
	tokens      int
	calls       []string
	expiresIn   int
	rejectToken string // token que la Admin API rechaza con 401 una vez
	known       uuid.UUID
}

func (f *fakeKeycloak) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /realms/petpassport/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "petpassport-backend" ||
			r.Form.Get("client_secret") != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.tokens++
		n := f.tokens
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-" + strconv.Itoa(n), "expires_in": f.expiresIn})
	})
	mux.HandleFunc("/admin/realms/petpassport/", func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		if auth == f.rejectToken {
			f.rejectToken = ""
			f.mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+r.URL.RawQuery+" "+string(body))
		f.mu.Unlock()
		if !strings.Contains(r.URL.Path, f.known.String()) && r.URL.Query().Get("email") == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]string{{"id": f.known.String()}})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func newClient(t *testing.T, f *fakeKeycloak, now func() time.Time) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL + "/", Realm: "petpassport", ClientID: "petpassport-backend", ClientSecret: "s3cret", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClient_OperationsUseAdminEndpoints(t *testing.T) {
	user := uuid.MustParse("01930000-0000-7000-8000-00000000a001")
	f := &fakeKeycloak{expiresIn: 300, known: user}
	c := newClient(t, f, time.Now)
	ctx := context.Background()
	if err := c.SetEnabled(ctx, user, false); err != nil {
		t.Fatal(err)
	}
	if err := c.LogoutAllSessions(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := c.SendVerifyEmail(ctx, user); err != nil {
		t.Fatal(err)
	}
	id, err := c.FindByEmail(ctx, "ana@example.com")
	if err != nil || id != user {
		t.Fatalf("FindByEmail: %v %v", id, err)
	}
	want := []string{
		"PUT /admin/realms/petpassport/users/" + user.String() + `  {"enabled":false}`,
		"POST /admin/realms/petpassport/users/" + user.String() + "/logout  ",
		"PUT /admin/realms/petpassport/users/" + user.String() + "/send-verify-email  ",
		"GET /admin/realms/petpassport/users email=ana%40example.com&exact=true ",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("llamadas:\n%s\nesperaba:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	if f.tokens != 1 {
		t.Fatalf("el token debe reutilizarse: %d emisiones", f.tokens)
	}
}

func TestClient_TokenRenewedSixtySecondsBeforeExpiry(t *testing.T) {
	user := uuid.New()
	f := &fakeKeycloak{expiresIn: 300, known: user}
	clock := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	c := newClient(t, f, func() time.Time { return clock })
	ctx := context.Background()
	_ = c.SetEnabled(ctx, user, true)
	clock = clock.Add(239 * time.Second)
	_ = c.SetEnabled(ctx, user, true)
	if f.tokens != 1 {
		t.Fatalf("a los 239 s el token sigue vigente: %d emisiones", f.tokens)
	}
	clock = clock.Add(time.Second) // 240 s = 300 - 60
	_ = c.SetEnabled(ctx, user, true)
	if f.tokens != 2 {
		t.Fatalf("a los 240 s debe renovarse: %d emisiones", f.tokens)
	}
}

func TestClient_RetriesOnceOn401(t *testing.T) {
	user := uuid.New()
	f := &fakeKeycloak{expiresIn: 300, known: user, rejectToken: "tok-1"}
	c := newClient(t, f, time.Now)
	if err := c.SetEnabled(context.Background(), user, false); err != nil {
		t.Fatal(err)
	}
	if f.tokens != 2 || len(f.calls) != 1 {
		t.Fatalf("tokens=%d calls=%v", f.tokens, f.calls)
	}
}

func TestClient_UnknownUserAndErrors(t *testing.T) {
	f := &fakeKeycloak{expiresIn: 300, known: uuid.New()}
	c := newClient(t, f, time.Now)
	if err := c.SetEnabled(context.Background(), uuid.New(), false); !errors.Is(err, app.ErrKeycloakUserNotFound) {
		t.Fatalf("usuario desconocido: %v", err)
	}
	bad, err := New(Options{BaseURL: c.opts.BaseURL, Realm: "petpassport", ClientID: "petpassport-backend", ClientSecret: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.SetEnabled(context.Background(), f.known, false); err == nil || errors.Is(err, app.ErrKeycloakUserNotFound) {
		t.Fatalf("credenciales inválidas deben fallar: %v", err)
	}
	if _, err := New(Options{BaseURL: "", Realm: "r", ClientID: "c", ClientSecret: "s"}); err == nil {
		t.Fatal("configuración incompleta debe fallar")
	}
}
