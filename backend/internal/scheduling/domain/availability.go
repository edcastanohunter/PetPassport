package domain

import (
	"sort"
	"time"
)

// Reservation es una franja ya reservada de un profesional (slot_reservations).
type Reservation struct {
	Start, End time.Time
}

// AvailableSlot es una franja libre ofrecida.
type AvailableSlot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// AvailabilityQuery son los parámetros del cálculo.
type AvailabilityQuery struct {
	Type     AppointmentType
	From, To time.Time // ventana pedida [From, To)
	Now      time.Time
	Reserved []Reservation
	// Guardian aplica las antelaciones mínima/máxima de la agenda: los
	// tutores solo ven franjas dentro de [now+min, now+max]. El personal ve
	// todo lo futuro.
	Guardian bool
}

func (a *ProfessionalAgenda) location() *time.Location {
	loc, err := time.LoadLocation(a.cfg.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// rangesFor devuelve los rangos horarios efectivos de una fecha local:
// la excepción manda sobre la plantilla semanal.
func (a *ProfessionalAgenda) rangesFor(day time.Time) []TimeRange {
	if ex, ok := a.exceptions[day.Format("2006-01-02")]; ok {
		if ex.Closed {
			return nil
		}
		return ex.Ranges
	}
	return a.cfg.WeeklyTemplate[weekdayKey[day.Weekday()]]
}

func localAt(day time.Time, minutes int, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, loc)
}

// Availability calcula las franjas libres (DOM-011): plantilla − excepciones
// − reservas activas, discretizada por SlotMinutes[tipo]. Es informativa: la
// autoridad es la restricción de exclusión al confirmar (ADR-011). Pura, sin
// I/O, para probarla por tabla.
func (a *ProfessionalAgenda) Availability(q AvailabilityQuery) []AvailableSlot {
	if !a.defined || !a.active || !q.To.After(q.From) {
		return nil
	}
	loc := a.location()
	step := time.Duration(a.SlotMinutesFor(q.Type)) * time.Minute
	earliest, latest := q.Now, q.To
	if q.Guardian {
		earliest = q.Now.Add(time.Duration(a.cfg.LeadMinHours) * time.Hour)
		if l := q.Now.Add(time.Duration(a.cfg.LeadMaxDays) * 24 * time.Hour); l.Before(latest) {
			latest = l
		}
	}
	if q.From.After(earliest) {
		earliest = q.From
	}
	reserved := append([]Reservation(nil), q.Reserved...)
	sort.Slice(reserved, func(i, j int) bool { return reserved[i].Start.Before(reserved[j].Start) })

	var out []AvailableSlot
	first := q.From.In(loc)
	last := q.To.In(loc)
	for day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc); !day.After(last); day = day.AddDate(0, 0, 1) {
		for _, r := range a.rangesFor(day) {
			s, e, ok := r.Minutes()
			if !ok {
				continue
			}
			end := localAt(day, e, loc)
			for start := localAt(day, s, loc); !start.Add(step).After(end); start = start.Add(step) {
				slotEnd := start.Add(step)
				if start.Before(earliest) || slotEnd.After(latest) {
					continue
				}
				if overlapsAny(start, slotEnd, reserved) {
					continue
				}
				out = append(out, AvailableSlot{Start: start.UTC(), End: slotEnd.UTC()})
			}
		}
	}
	return out
}

func overlapsAny(start, end time.Time, reserved []Reservation) bool {
	for _, r := range reserved {
		if r.Start.Before(end) && start.Before(r.End) {
			return true
		}
	}
	return false
}

// Covers indica si una franja cae completa dentro del horario de la agenda
// en su día local (A5, "dentro de disponibilidad"). No mira reservas.
func (a *ProfessionalAgenda) Covers(s Slot) bool {
	if !a.defined || !a.active {
		return false
	}
	loc := a.location()
	start, end := s.Start.In(loc), s.End.In(loc)
	if start.Year() != end.Year() || start.YearDay() != end.YearDay() {
		return false
	}
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	for _, r := range a.rangesFor(day) {
		rs, re, ok := r.Minutes()
		if !ok {
			continue
		}
		if !start.Before(localAt(day, rs, loc)) && !end.After(localAt(day, re, loc)) {
			return true
		}
	}
	return false
}
