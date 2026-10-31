package domain

import (
	"fmt"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Offset es un recordatorio expresado como antelación sobre el instante de
// referencia (T-24h, T-2h, T-14d…). El texto es la parte `offset` de la clave
// de idempotencia.
type Offset struct {
	Label  string
	Before time.Duration
}

// Desplazamientos de F2 (PRD-024).
var (
	AppointmentOffsets = []Offset{{"T-24h", 24 * time.Hour}, {"T-2h", 2 * time.Hour}}
	VaccineOffsets     = []Offset{{"T-14d", 14 * 24 * time.Hour}, {"T-0", 0}}
	FollowUpOffsets    = []Offset{{"T-3d", 3 * 24 * time.Hour}, {"T-0", 0}}
)

// ReminderHour es la hora local a la que salen los recordatorios por fecha.
const ReminderHour = 9

// Due es un aviso a programar.
type Due struct {
	Offset string
	At     time.Time
}

// AppointmentDue calcula los instantes de aviso de una cita en su franja. Los
// que ya pasaron respecto a now se omiten (una cita creada para dentro de 3 h
// no recibe un T-24h retroactivo).
func AppointmentDue(start, now time.Time) []Due {
	var out []Due
	for _, o := range AppointmentOffsets {
		at := start.Add(-o.Before)
		if at.After(now) {
			out = append(out, Due{Offset: o.Label, At: at})
		}
	}
	return out
}

// DateDue calcula los avisos para un hito de fecha civil (vacuna o control):
// a las 09:00 hora local de la zona en cada día resultante (NOT-002). Los
// pasados se omiten.
func DateDue(date kernel.CivilDate, tz string, offsets []Offset, now time.Time) ([]Due, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("notifications: zona horaria %q: %w", tz, err)
	}
	d := date.Time()
	if d.IsZero() {
		return nil, fmt.Errorf("notifications: fecha civil inválida %q", date)
	}
	var out []Due
	for _, o := range offsets {
		day := d.Add(-o.Before)
		at := time.Date(day.Year(), day.Month(), day.Day(), ReminderHour, 0, 0, 0, loc)
		if at.After(now) {
			out = append(out, Due{Offset: o.Label, At: at.UTC()})
		}
	}
	return out, nil
}

// TimeZoneFor propone la zona horaria civil de un usuario o clínica a partir
// del país (los usuarios no guardan zona propia). Coincide con la que usa
// Scheduling al crear agendas; sin país conocido, la del piloto.
func TimeZoneFor(country string) string {
	switch country {
	case "CO", "":
		return "America/Bogota"
	case "MX":
		return "America/Mexico_City"
	case "AR":
		return "America/Argentina/Buenos_Aires"
	case "CL":
		return "America/Santiago"
	case "PE":
		return "America/Lima"
	case "EC":
		return "America/Guayaquil"
	case "ES":
		return "Europe/Madrid"
	}
	return "UTC"
}
