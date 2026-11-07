package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// CanonicalJSON serializa un payload JSON según RFC 8785 (JCS): claves de
// objeto ordenadas, sin espacios, números en la forma canónica de
// ECMAScript y cadenas UTF-8 con los escapes mínimos (PP-F4-ARQ-011).
func CanonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("canonical json: contenido extra tras el primer valor")
	}
	var b bytes.Buffer
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeString(b, x)
	case json.Number:
		n, err := canonicalNumber(x)
		if err != nil {
			return err
		}
		b.WriteString(n)
	case float64:
		n, err := canonicalFloat(x)
		if err != nil {
			return err
		}
		b.WriteString(n)
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("canonical json: tipo no soportado %T", v)
	}
	return nil
}

func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

func canonicalNumber(n json.Number) (string, error) {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return "", fmt.Errorf("canonical json: número %q: %w", n, err)
	}
	return canonicalFloat(f)
}

func canonicalFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", errors.New("canonical json: NaN/Infinito no representable")
	}
	if f == 0 {
		return "0", nil
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	if i := strings.IndexByte(s, 'e'); i >= 0 {
		mantissa, exp := s[:i], s[i+1:]
		sign := ""
		if len(exp) > 0 && (exp[0] == '+' || exp[0] == '-') {
			sign, exp = exp[:1], exp[1:]
		}
		exp = strings.TrimLeft(exp, "0")
		if exp == "" {
			exp = "0"
		}
		s = mantissa + "e" + sign + exp
	}
	return s, nil
}

// HashableEvent es la vista mínima de un evento persistido que entra en el ContentHash.
type HashableEvent struct {
	EventID       uuid.UUID
	EventType     string
	SchemaVersion int
	Payload       json.RawMessage
}

// ContentHash calcula el SHA-256 de los eventos previos hasta la emisión de la receta (PP-F4-ARQ-011).
func ContentHash(events []HashableEvent) (string, error) {
	h := sha256.New()
	for _, e := range events {
		if e.EventID == uuid.Nil || e.EventType == "" || e.SchemaVersion < 1 {
			return "", errors.New("content hash: evento incompleto")
		}
		canon, err := CanonicalJSON(e.Payload)
		if err != nil {
			return "", fmt.Errorf("content hash: %s: %w", e.EventType, err)
		}
		_, _ = fmt.Fprintf(h, "%s|%s|%d|", e.EventID, e.EventType, e.SchemaVersion)
		h.Write(canon)
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
