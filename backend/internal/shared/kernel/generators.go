package kernel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
)

// Generadores inyectables (docs/phase-1/02 §6 DOM-010): Clock, IDGen y
// TokenGen se inyectan para que las pruebas controlen tiempo e identidad.

// Clock entrega el instante actual.
type Clock interface {
	Now() time.Time
}

// RealClock usa time.Now (producción).
type RealClock struct{}

// Now implementa Clock.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// IDGenerator genera UUIDv7 (ADR-007).
type IDGenerator interface {
	NewID() uuid.UUID
}

// RealIDGenerator usa kernel.NewID (producción).
type RealIDGenerator struct{}

// NewID implementa IDGenerator.
func (RealIDGenerator) NewID() uuid.UUID { return NewID() }

// TokenGenerator genera tokens de un solo uso (invitaciones, transferencias):
// 32 bytes aleatorios en base64url. Solo se persiste HashToken(token).
type TokenGenerator interface {
	NewToken() string
}

// RealTokenGenerator usa crypto/rand (producción).
type RealTokenGenerator struct{}

// NewToken implementa TokenGenerator.
func (RealTokenGenerator) NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("kernel: rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken devuelve el SHA-256 del token en crudo (lo único que se guarda).
func HashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}
