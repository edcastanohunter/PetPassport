package kernel

import (
	"errors"
	"time"
)

// ErrInvalidDate indica una fecha civil mal formada.
var ErrInvalidDate = errors.New("invalid date")

// CivilDate es una fecha civil (sin zona horaria) en formato YYYY-MM-DD
// (docs/phase-1/02 §3.2: BirthDate usa "fecha civil"). Se representa como
// string canónico para evitar problemas de TZ al persistir y proyectar.
type CivilDate string

const civilLayout = "2006-01-02"

// NewCivilDate valida y canóniza (rechaza "2021-5-1").
func NewCivilDate(s string) (CivilDate, error) {
	if _, err := time.Parse(civilLayout, s); err != nil {
		return "", ErrInvalidDate
	}
	return CivilDate(s), nil
}

// CivilDateOf convierte un time.Time a fecha civil en UTC.
func CivilDateOf(t time.Time) CivilDate {
	return CivilDate(t.UTC().Format(civilLayout))
}

// Time devuelve la fecha como time.Time UTC a medianoche.
func (d CivilDate) Time() time.Time {
	t, err := time.Parse(civilLayout, string(d))
	if err != nil {
		return time.Time{}
	}
	return t
}

func (d CivilDate) String() string { return string(d) }

// Before compara fechas civiles.
func (d CivilDate) Before(other CivilDate) bool { return string(d) < string(other) }

// After compara fechas civiles.
func (d CivilDate) After(other CivilDate) bool { return string(d) > string(other) }

// AddDays suma días a la fecha civil.
func (d CivilDate) AddDays(days int) CivilDate {
	return CivilDateOf(d.Time().AddDate(0, 0, days))
}
