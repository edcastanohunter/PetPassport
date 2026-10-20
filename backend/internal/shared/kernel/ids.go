// Package kernel contiene los tipos compartidos por todos los bounded contexts:
// identificadores, errores de dominio y value objects comunes. No importa nada
// fuera de la biblioteca estándar y de uuid (ADR-001, ADR-007).
package kernel

import "github.com/google/uuid"

// NewID genera un UUIDv7 (ADR-007).
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// ParseID valida un identificador recibido del exterior.
func ParseID(s string) (uuid.UUID, error) { return uuid.Parse(s) }
