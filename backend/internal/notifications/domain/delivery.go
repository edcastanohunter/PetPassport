package domain

import "time"

// MaxAttempts es el tope de intentos de envío (NOT-005): superado, la entrega
// pasa a failed definitivo (dead-letter).
const MaxAttempts = 5

// backoff[i] es la espera tras el intento i+1 fallido: 30 s, 2 m, 10 m, 30 m, 2 h.
var backoff = [MaxAttempts]time.Duration{
	30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour,
}

// NextAttempt decide qué hacer tras el intento número attempt (1-based) fallido
// y reintentable: la espera hasta el siguiente o dead=true si ya no quedan.
func NextAttempt(attempt int) (wait time.Duration, dead bool) {
	if attempt >= MaxAttempts {
		return 0, true
	}
	if attempt < 1 {
		attempt = 1
	}
	return backoff[attempt-1], false
}

// ThrottleWindow: máximo 1 email por (destinatario, plantilla) en esta ventana
// (07 §1), salvo security y los recordatorios de cita.
const ThrottleWindow = 60 * time.Minute

// TemplateAppointmentReminder está exento de la limitación (07 §1).
const TemplateAppointmentReminder = "appointment_reminder"

// Throttled indica si la plantilla/categoría está sujeta a la limitación.
func Throttled(template string, c Category) bool {
	return c != Security && template != TemplateAppointmentReminder
}

// Estados de una entrega (DAT-005).
const (
	StatusQueued     = "queued"
	StatusSending    = "sending"
	StatusSent       = "sent"
	StatusFailed     = "failed"
	StatusSuppressed = "suppressed"
)
