// Package export produce el respaldo frío de es.events (PP-F1-DAT-009): una
// exportación completa en JSONL comprimido con gzip, independiente del
// proveedor de PostgreSQL, y su verificación para el ensayo de restauración.
//
// Formato (pp-es-events-jsonl-gzip/v1): una línea JSON por evento, en orden
// estricto de global_position, con payload y metadata tal cual están en la BD.
// El manifiesto acompaña al fichero con el recuento, el rango de posiciones y
// el sha256 del .jsonl.gz.
package export

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// Format identifica la versión del formato de exportación.
const Format = "pp-es-events-jsonl-gzip/v1"

// Line es un evento exportado (una línea del JSONL).
type Line struct {
	GlobalPosition int64           `json:"global_position"`
	EventID        uuid.UUID       `json:"event_id"`
	StreamID       uuid.UUID       `json:"stream_id"`
	StreamType     string          `json:"stream_type"`
	StreamVersion  int64           `json:"stream_version"`
	EventType      string          `json:"event_type"`
	SchemaVersion  int16           `json:"schema_version"`
	Payload        json.RawMessage `json:"payload"`
	Metadata       json.RawMessage `json:"metadata"`
	OccurredAt     time.Time       `json:"occurred_at"`
	RecordedAt     time.Time       `json:"recorded_at"`
}

// Manifest describe una exportación. Sin PII: solo recuentos y huellas.
type Manifest struct {
	Format        string    `json:"format"`
	Events        int64     `json:"events"`
	FirstPosition int64     `json:"first_position"`
	LastPosition  int64     `json:"last_position"`
	Bytes         int64     `json:"bytes"`
	SHA256        string    `json:"sha256"`
	StartedAt     time.Time `json:"started_at,omitzero"`
	FinishedAt    time.Time `json:"finished_at,omitzero"`
}

// Source entrega eventos por posición global creciente (keyset).
type Source interface {
	Batch(ctx context.Context, after int64, limit int) ([]Line, error)
}

// DefaultBatch es el tamaño de lote de lectura.
const DefaultBatch = 5000

// Write exporta todos los eventos de src a w (gzip) y devuelve el manifiesto.
func Write(ctx context.Context, src Source, w io.Writer, batch int, now func() time.Time) (Manifest, error) {
	if batch <= 0 {
		batch = DefaultBatch
	}
	m := Manifest{Format: Format, StartedAt: now().UTC()}
	sum := sha256.New()
	counted := &countingWriter{w: io.MultiWriter(w, sum)}
	gz, err := gzip.NewWriterLevel(counted, gzip.BestCompression)
	if err != nil {
		return Manifest{}, err
	}
	enc := json.NewEncoder(gz)
	after := int64(0)
	for {
		lines, err := src.Batch(ctx, after, batch)
		if err != nil {
			return Manifest{}, fmt.Errorf("export: lote tras %d: %w", after, err)
		}
		for i := range lines {
			l := &lines[i]
			if l.GlobalPosition <= after {
				return Manifest{}, fmt.Errorf("export: posición %d no creciente", l.GlobalPosition)
			}
			if err := enc.Encode(l); err != nil {
				return Manifest{}, fmt.Errorf("export: escribir %d: %w", l.GlobalPosition, err)
			}
			if m.Events == 0 {
				m.FirstPosition = l.GlobalPosition
			}
			m.Events++
			m.LastPosition = l.GlobalPosition
			after = l.GlobalPosition
		}
		if len(lines) < batch {
			break
		}
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, fmt.Errorf("export: cerrar gzip: %w", err)
	}
	m.Bytes = counted.n
	m.SHA256 = hex.EncodeToString(sum.Sum(nil))
	m.FinishedAt = now().UTC()
	return m, nil
}

// ErrCorrupt indica que el fichero no es una exportación válida.
var ErrCorrupt = errors.New("export: fichero corrupto")

// Verify relee una exportación completa: gzip íntegro, cada línea decodifica,
// posiciones estrictamente crecientes y campos obligatorios presentes. Devuelve
// el manifiesto recalculado (sin fechas) para compararlo con el publicado.
func Verify(r io.Reader) (Manifest, error) {
	return read(r, nil, nil)
}

// read recorre y valida una exportación; each (opcional) recibe cada evento
// válido y done (opcional) se llama al final si todo el fichero es válido.
func read(r io.Reader, each func(*Line) error, done func() error) (Manifest, error) {
	sum := sha256.New()
	counted := &countingReader{r: io.TeeReader(r, sum)}
	gz, err := gzip.NewReader(counted)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	m := Manifest{Format: Format}
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	after := int64(0)
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return Manifest{}, fmt.Errorf("%w: línea %d: %v", ErrCorrupt, m.Events+1, err)
		}
		if l.GlobalPosition <= after {
			return Manifest{}, fmt.Errorf("%w: posición %d tras %d", ErrCorrupt, l.GlobalPosition, after)
		}
		if l.EventID == uuid.Nil || l.StreamID == uuid.Nil || l.EventType == "" || len(l.Payload) == 0 {
			return Manifest{}, fmt.Errorf("%w: evento incompleto en posición %d", ErrCorrupt, l.GlobalPosition)
		}
		if each != nil {
			if err := each(&l); err != nil {
				return Manifest{}, fmt.Errorf("export: posición %d: %w", l.GlobalPosition, err)
			}
		}
		if m.Events == 0 {
			m.FirstPosition = l.GlobalPosition
		}
		m.Events++
		m.LastPosition = l.GlobalPosition
		after = l.GlobalPosition
	}
	if err := sc.Err(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	// Consumir el resto para que la huella cubra el fichero entero.
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return Manifest{}, err
	}
	m.Bytes = counted.n
	m.SHA256 = hex.EncodeToString(sum.Sum(nil))
	if done != nil {
		if err := done(); err != nil {
			return Manifest{}, err
		}
	}
	return m, nil
}

// Matches compara un manifiesto recalculado con el publicado (sin fechas).
func (m Manifest) Matches(other Manifest) bool {
	return m.Format == other.Format && m.Events == other.Events &&
		m.FirstPosition == other.FirstPosition && m.LastPosition == other.LastPosition &&
		m.Bytes == other.Bytes && m.SHA256 == other.SHA256
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
