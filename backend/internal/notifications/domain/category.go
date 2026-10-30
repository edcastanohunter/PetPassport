// Package domain contiene las reglas puras de Notifications (ADR-012): categorías
// y preferencias, claves de idempotencia, token de baja, política de reintentos
// y cálculo de recordatorios. No toca base de datos ni SMTP.
package domain

import (
	"encoding/json"
	"fmt"
)

// Category agrupa notificaciones para las preferencias del usuario (NOT-004).
type Category string

// Categorías de F2. security no se puede desactivar.
const (
	Appointments   Category = "appointments"
	PreventiveCare Category = "preventive_care"
	Clinical       Category = "clinical"
	Account        Category = "account"
	Security       Category = "security"
)

// All lista las categorías en el orden de presentación.
func All() []Category {
	return []Category{Appointments, PreventiveCare, Clinical, Account, Security}
}

// Valid indica si c es una categoría conocida.
func (c Category) Valid() bool {
	for _, k := range All() {
		if k == c {
			return true
		}
	}
	return false
}

// Optional indica si el usuario puede desactivar la categoría.
func (c Category) Optional() bool { return c.Valid() && c != Security }

// Preferences son las preferencias de un usuario (canal email).
type Preferences struct {
	Email  map[Category]bool `json:"email"`
	Digest Digest            `json:"digest"`
}

// Digest agrupa los resúmenes opcionales.
type Digest struct {
	ClinicDailyAgenda bool `json:"clinic_daily_agenda"`
}

// DefaultPreferences activa todas las categorías y ningún resumen.
func DefaultPreferences() Preferences {
	p := Preferences{Email: map[Category]bool{}}
	for _, c := range All() {
		p.Email[c] = true
	}
	return p
}

// Allows indica si el usuario quiere recibir la categoría. Security siempre.
func (p Preferences) Allows(c Category) bool {
	if c == Security {
		return true
	}
	v, ok := p.Email[c]
	return !ok || v // ausente = valor por defecto (activa)
}

// Normalize devuelve las preferencias saneadas: security fuerza true, se
// ignoran categorías desconocidas y las ausentes toman el valor por defecto.
func (p Preferences) Normalize() Preferences {
	out := DefaultPreferences()
	for c, v := range p.Email {
		if c.Optional() {
			out.Email[c] = v
		}
	}
	out.Digest = p.Digest
	return out
}

// ParsePreferences lee el JSON de las columnas email/digest.
func ParsePreferences(email, digest []byte) (Preferences, error) {
	p := Preferences{Email: map[Category]bool{}}
	if len(email) > 0 {
		if err := json.Unmarshal(email, &p.Email); err != nil {
			return Preferences{}, fmt.Errorf("notifications: preferencias email: %w", err)
		}
	}
	if len(digest) > 0 {
		if err := json.Unmarshal(digest, &p.Digest); err != nil {
			return Preferences{}, fmt.Errorf("notifications: preferencias digest: %w", err)
		}
	}
	return p.Normalize(), nil
}
