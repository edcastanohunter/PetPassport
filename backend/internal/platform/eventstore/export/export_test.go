package export

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PP-F1-DAT-009: la exportación semanal de es.events es completa, ordenada y
// verificable sin acceso a la base de datos.

type memSource struct {
	lines   []Line
	batches int
}

func (m *memSource) Batch(_ context.Context, after int64, limit int) ([]Line, error) {
	m.batches++
	var out []Line
	for _, l := range m.lines {
		if l.GlobalPosition > after && len(out) < limit {
			out = append(out, l)
		}
	}
	return out, nil
}

func events(n int, gaps bool) []Line {
	out := make([]Line, n)
	pos := int64(0)
	for i := range out {
		pos++
		if gaps && i%3 == 0 {
			pos++ // las IDENTITY pueden tener huecos (rollbacks)
		}
		out[i] = Line{
			GlobalPosition: pos, EventID: uuid.New(), StreamID: uuid.New(), StreamType: "Pet",
			StreamVersion: 1, EventType: "PetRegistered", SchemaVersion: 1,
			Payload:    json.RawMessage(`{"name":"Luna"}`),
			Metadata:   json.RawMessage(`{"source":"api"}`),
			OccurredAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
			RecordedAt: time.Date(2026, 10, 1, 10, 0, 1, 0, time.UTC),
		}
	}
	return out
}

func clock() func() time.Time {
	t := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func TestExport_DAT009_RoundTripAcrossBatches(t *testing.T) {
	src := &memSource{lines: events(25, true)}
	var buf bytes.Buffer
	m, err := Write(context.Background(), src, &buf, 10, clock())
	if err != nil {
		t.Fatal(err)
	}
	if m.Events != 25 || m.FirstPosition != 2 || m.LastPosition != src.lines[24].GlobalPosition {
		t.Fatalf("manifiesto inesperado: %+v", m)
	}
	if src.batches != 3 {
		t.Fatalf("lotes = %d, want 3", src.batches)
	}
	if m.Bytes != int64(buf.Len()) || len(m.SHA256) != 64 || m.Format != Format {
		t.Fatalf("huella inesperada: %+v (len %d)", m, buf.Len())
	}

	got, err := Verify(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matches(m) {
		t.Fatalf("Verify = %+v, want %+v", got, m)
	}

	// El contenido conserva payload y metadata tal cual.
	gz, _ := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	var first Line
	if err := json.NewDecoder(gz).Decode(&first); err != nil {
		t.Fatal(err)
	}
	if string(first.Payload) != `{"name":"Luna"}` || first.EventID != src.lines[0].EventID {
		t.Fatalf("primera línea = %+v", first)
	}
}

func TestExport_DAT009_EmptyStore(t *testing.T) {
	var buf bytes.Buffer
	m, err := Write(context.Background(), &memSource{}, &buf, 0, clock())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if m.Events != 0 || !got.Matches(m) {
		t.Fatalf("vacío: %+v vs %+v", m, got)
	}
}

func gzipLines(t *testing.T, lines ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(strings.Join(lines, "\n") + "\n"))
	_ = gz.Close()
	return buf.Bytes()
}

func TestExport_DAT009_VerifyRejectsCorruption(t *testing.T) {
	good := events(2, false)
	a, _ := json.Marshal(good[0])
	b, _ := json.Marshal(good[1])
	incomplete := `{"global_position":9,"event_id":"` + uuid.NewString() + `"}`

	var valid bytes.Buffer
	if _, err := Write(context.Background(), &memSource{lines: good}, &valid, 0, clock()); err != nil {
		t.Fatal(err)
	}
	truncated := valid.Bytes()[:valid.Len()-6]

	cases := map[string][]byte{
		"desordenado": gzipLines(t, string(b), string(a)),
		"duplicado":   gzipLines(t, string(a), string(a)),
		"incompleto":  gzipLines(t, incomplete),
		"no json":     gzipLines(t, "esto no es json"),
		"no gzip":     []byte("plain text"),
		"truncado":    truncated,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(bytes.NewReader(data)); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestExport_DAT009_WriteRejectsNonIncreasingSource(t *testing.T) {
	lines := events(2, false)
	lines[1].GlobalPosition = lines[0].GlobalPosition
	if _, err := Write(context.Background(), fixedSource(lines), &bytes.Buffer{}, 10, clock()); err == nil {
		t.Fatal("se esperaba error por posición no creciente")
	}
}

type fixedSource []Line

func (f fixedSource) Batch(_ context.Context, after int64, _ int) ([]Line, error) {
	if after > 0 {
		return nil, nil
	}
	return f, nil
}

func TestExport_DAT009_ManifestMatchesIgnoresDates(t *testing.T) {
	a := Manifest{Format: Format, Events: 1, FirstPosition: 1, LastPosition: 1, Bytes: 10, SHA256: "x",
		StartedAt: time.Now()}
	b := a
	b.StartedAt = time.Time{}
	if !a.Matches(b) {
		t.Fatal("las fechas no deben contar")
	}
	b.SHA256 = "y"
	if a.Matches(b) {
		t.Fatal("una huella distinta no coincide")
	}
}
