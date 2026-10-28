package maintenance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeDB simula platform.maintenance: active decide si hay fila; err falla la consulta.
type fakeDB struct {
	active  bool
	err     error
	queries int
}

func (f *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	f.queries++
	return fakeRow{f}
}

type fakeRow struct{ f *fakeDB }

func (r fakeRow) Scan(dest ...any) error {
	switch {
	case r.f.err != nil:
		return r.f.err
	case !r.f.active:
		return pgx.ErrNoRows
	}
	*dest[0].(*string) = "rebuild --sync"
	*dest[1].(*time.Time) = time.Unix(0, 0)
	return nil
}

func TestGate_CachesForTTLAndKeepsLastStateOnError(t *testing.T) {
	db := &fakeDB{}
	g := NewGate(db, time.Minute)
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	ctx := context.Background()

	if g.Active(ctx) {
		t.Fatal("sin fila: inactivo")
	}
	db.active = true
	if g.Active(ctx) || db.queries != 1 {
		t.Fatalf("dentro del TTL no consulta (queries=%d)", db.queries)
	}
	now = now.Add(time.Minute)
	if !g.Active(ctx) {
		t.Fatal("tras el TTL ve el mantenimiento")
	}
	db.err = errors.New("conexión perdida")
	now = now.Add(time.Minute)
	if !g.Active(ctx) {
		t.Fatal("un error de BD no saca del mantenimiento")
	}
	db.err, db.active = nil, false
	now = now.Add(time.Minute)
	if g.Active(ctx) {
		t.Fatal("al borrar la fila vuelve a servir")
	}
}

func TestGate_Middleware(t *testing.T) {
	db := &fakeDB{active: true}
	h := NewGate(db, 0).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for path, want := range map[string]int{
		"/api/v1/pets":    http.StatusServiceUnavailable,
		"/api/v1/healthz": http.StatusNoContent,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, esperaba %d", path, rec.Code, want)
		}
		if want == http.StatusServiceUnavailable &&
			(rec.Header().Get("Retry-After") != "30" || rec.Header().Get("Content-Type") != "application/problem+json") {
			t.Errorf("%s: cabeceras %v", path, rec.Header())
		}
	}
}
