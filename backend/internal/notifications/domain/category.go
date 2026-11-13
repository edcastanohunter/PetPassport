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

// Categorías. security y billing (F5, PP-F5-API: «no desactivable») no se
// pueden desactivar.
const (
	Appointments   Category = "appointments"
	PreventiveCare Category = "preventive_care"
	Clinical       Category = "clinical"
	Account        Category = "account"
	Security       Category = "security"
	Billing        Category = "billing"
)

// All lista las categorías en el orden de presentación.
func All() []Category {
	return []Category{Appointments, PreventiveCare, Clinical, Account, Security, Billing}
}

// Mandatory indica si la categoría se envía siempre, sin preferencia ni baja.
func (c Category) Mandatory() bool { return c == Security || c == Billing }

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
func (c Category) Optional() bool { return c.Valid() && !c.Mandatory() }

// Preferences son las preferencias de un usuario (canales email y push, PP-F3-DOM-011, PP-F3-API-008).
type Preferences struct {
	Email  map[Category]bool `json:"email"`
	Push   map[Category]bool `json:"push"`
	Digest Digest            `json:"digest"`
}

// Digest agrupa los resúmenes opcionales.
type Digest struct {
	ClinicDailyAgenda bool `json:"clinic_daily_agenda"`
}

// DefaultPushPreferences activa las categorías por defecto para push (PP-F3-DAT-006).
func DefaultPushPreferences() map[Category]bool {
	return map[Category]bool{
		Appointments:   true,
		PreventiveCare: true,
		Clinical:       false,
		Account:        false,
		Security:       true,
		Billing:        true,
	}
}

// DefaultPreferences activa todas las categorías de email y las por defecto de push.
func DefaultPreferences() Preferences {
	p := Preferences{
		Email: map[Category]bool{},
		Push:  DefaultPushPreferences(),
	}
	for _, c := range All() {
		p.Email[c] = true
	}
	return p
}

// Allows indica si el usuario quiere recibir la categoría por email. Security siempre.
func (p Preferences) Allows(c Category) bool {
	return p.AllowsEmail(c)
}

// AllowsEmail indica si el usuario quiere recibir la categoría por email.
func (p Preferences) AllowsEmail(c Category) bool {
	if c.Mandatory() {
		return true
	}
	v, ok := p.Email[c]
	return !ok || v
}

// AllowsPush indica si el usuario quiere recibir la categoría por push.
func (p Preferences) AllowsPush(c Category) bool {
	if c.Mandatory() {
		return true
	}
	if p.Push == nil {
		def := DefaultPushPreferences()
		return def[c]
	}
	v, ok := p.Push[c]
	if !ok {
		def := DefaultPushPreferences()
		return def[c]
	}
	return v
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
	if p.Push != nil {
		for c, v := range p.Push {
			if c.Optional() {
				out.Push[c] = v
			}
		}
	}
	for _, c := range All() {
		if c.Mandatory() {
			out.Email[c] = true
			out.Push[c] = true
		}
	}
	out.Digest = p.Digest
	return out
}

// ParsePreferences lee el JSON de las columnas email/push/digest.
func ParsePreferences(email, push, digest []byte) (Preferences, error) {
	p := Preferences{
		Email: map[Category]bool{},
		Push:  DefaultPushPreferences(),
	}
	if len(email) > 0 {
		if err := json.Unmarshal(email, &p.Email); err != nil {
			return Preferences{}, fmt.Errorf("notifications: preferencias email: %w", err)
		}
	}
	if len(push) > 0 {
		if err := json.Unmarshal(push, &p.Push); err != nil {
			return Preferences{}, fmt.Errorf("notifications: preferencias push: %w", err)
		}
	}
	if len(digest) > 0 {
		if err := json.Unmarshal(digest, &p.Digest); err != nil {
			return Preferences{}, fmt.Errorf("notifications: preferencias digest: %w", err)
		}
	}
	return p.Normalize(), nil
}
