package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeRow struct {
	val []byte
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*[]byte)) = r.val
	return nil
}

// fakeQ responde por prefijo de la consulta; sin entrada → pgx.ErrNoRows.
type fakeQ struct {
	rows  map[string][]byte
	calls []string
}

func (q *fakeQ) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls = append(q.calls, sql)
	for frag, v := range q.rows {
		if strings.Contains(sql, frag) {
			return fakeRow{val: v}
		}
	}
	return fakeRow{err: pgx.ErrNoRows}
}

func TestBuildExport_ComposesEverySectionAndSkipsEmptyOptionalOnes(t *testing.T) {
	id := uuid.New()
	q := &fakeQ{rows: map[string][]byte{
		"identity.user_accounts":       []byte(`{"id":"x","email":"ana@example.com"}`),
		"read_petregistry.pets p":      []byte(`[{"name":"Luna"}]`),
		"medical_timeline":             []byte(`[]`),
		"notifications.deliveries":     []byte(`[{"template":"transfer_done"}]`),
		"read_scheduling.appointments": []byte(`[]`),
	}}
	doc, err := buildExport(context.Background(), q, id, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("el documento debe ser JSON válido: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back["format"] != exportFormat || back["user_id"] != id.String() || back["exported_at"] != "2026-11-03T10:00:00Z" {
		t.Fatalf("cabecera: %v", back)
	}
	for _, s := range exportSections {
		if _, ok := back[s.key]; !ok {
			t.Errorf("falta la sección %q", s.key)
		}
	}
	if back["notification_preferences"] != nil {
		t.Errorf("una sección opcional sin fila debe quedar en null: %v", back["notification_preferences"])
	}
}

func TestBuildExport_UnknownUserFails(t *testing.T) {
	if _, err := buildExport(context.Background(), &fakeQ{rows: map[string][]byte{}}, uuid.New(), time.Now()); err == nil {
		t.Fatal("un usuario inexistente debe fallar")
	}
}

// El archivo sale de la plataforma: ninguna consulta debe pedir secretos ni
// claves internas de objeto.
func TestExportSections_NeverSelectSecrets(t *testing.T) {
	for _, s := range exportSections {
		for _, banned := range []string{"recipient_address_hash", "payload", "unsubscribe_token_hash"} {
			// Se admite nombrarlas solo para excluirlas (operador «-»).
			if strings.Contains(s.sql, banned) && !strings.Contains(s.sql, "- '"+banned+"'") {
				t.Errorf("la sección %s referencia %s", s.key, banned)
			}
		}
	}
}
