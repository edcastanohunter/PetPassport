// Package domain contiene el agregado Pet, sus value objects, comandos y
// eventos v1 (PP-F1-DOM-003/004/005, docs/phase-1/02 §3). Puro: solo stdlib,
// kernel y uuid (ADR-001 regla 1).
package domain

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Species es la especie de la mascota (DOM-004).
type Species string

// Especies admitidas en F1.
const (
	SpeciesDog     Species = "dog"
	SpeciesCat     Species = "cat"
	SpeciesBird    Species = "bird"
	SpeciesRabbit  Species = "rabbit"
	SpeciesReptile Species = "reptile"
	SpeciesRodent  Species = "rodent"
	SpeciesEquine  Species = "equine"
	SpeciesFerret  Species = "ferret"
	SpeciesFish    Species = "fish"
	SpeciesOther   Species = "other"
)

var speciesValues = map[Species]bool{
	SpeciesDog: true, SpeciesCat: true, SpeciesBird: true, SpeciesRabbit: true,
	SpeciesReptile: true, SpeciesRodent: true, SpeciesEquine: true, SpeciesFerret: true,
	SpeciesFish: true, SpeciesOther: true,
}

// NewSpecies valida y normaliza (lower-case).
func NewSpecies(s string) (Species, error) {
	v := Species(strings.ToLower(strings.TrimSpace(s)))
	if !speciesValues[v] {
		return "", invalid("species")
	}
	return v, nil
}

// IsOther informa si la especie exige texto libre.
func (s Species) IsOther() bool { return s == SpeciesOther }

// Sex es el sexo de la mascota.
type Sex string

// Valores admitidos.
const (
	SexMale    Sex = "male"
	SexFemale  Sex = "female"
	SexUnknown Sex = "unknown"
)

// NewSex valida y normaliza.
func NewSex(s string) (Sex, error) {
	v := Sex(strings.ToLower(strings.TrimSpace(s)))
	switch v {
	case SexMale, SexFemale, SexUnknown:
		return v, nil
	}
	return "", invalid("sex")
}

// Tristate es un booleano con estado desconocido.
type Tristate string

// Valores admitidos.
const (
	TristateYes     Tristate = "yes"
	TristateNo      Tristate = "no"
	TristateUnknown Tristate = "unknown"
)

// NewTristate valida y normaliza.
func NewTristate(s string) (Tristate, error) {
	v := Tristate(strings.ToLower(strings.TrimSpace(s)))
	switch v {
	case TristateYes, TristateNo, TristateUnknown:
		return v, nil
	}
	return "", invalid("neutered")
}

// PetName es el nombre de la mascota: 1–60 caracteres tras trim, Unicode
// letras/espacios/'/-, sin caracteres de control (I1).
type PetName string

// NewPetName valida y normaliza (trim + colapso de espacios). Los
// caracteres de control se rechazan antes de normalizar (I1).
func NewPetName(s string) (PetName, error) {
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", invalid("name")
		}
	}
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" || len([]rune(s)) > 60 {
		return "", invalid("name")
	}
	for _, r := range s {
		ok := unicode.IsLetter(r) || r == ' ' || r == '\'' || r == '-'
		if !ok {
			return "", invalid("name")
		}
	}
	return PetName(s), nil
}

// Breed es la raza: texto libre 1–80 en F1 (catálogo en F2).
type Breed string

// NewBreed valida y normaliza (trim).
func NewBreed(s string) (Breed, error) {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 80 {
		return "", invalid("breed")
	}
	return Breed(s), nil
}

// Markings son color/señas: ≤ 200 caracteres.
type Markings string

// NewMarkings valida y normaliza (trim).
func NewMarkings(s string) (Markings, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 200 {
		return "", invalid("markings")
	}
	return Markings(s), nil
}

// BirthDate es la fecha de nacimiento civil con indicador de aproximación
// (DOM-004): ≤ hoy y ≥ hoy − 40 años.
type BirthDate struct {
	Date        kernel.CivilDate
	Approximate bool
}

// MaxPetAgeYears es el límite de edad admitido (PRD-005).
const MaxPetAgeYears = 40

// NewBirthDate valida contra `today` (la fecha civil del instante del comando).
func NewBirthDate(date string, approximate bool, today kernel.CivilDate) (BirthDate, error) {
	cd, err := kernel.NewCivilDate(date)
	if err != nil {
		return BirthDate{}, invalid("birth_date")
	}
	min := CivilMinBirthDate(today)
	if cd.After(today) || cd.Before(min) {
		return BirthDate{}, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "birth_date", Code: "OUT_OF_RANGE"}}}
	}
	return BirthDate{Date: cd, Approximate: approximate}, nil
}

// CivilMinBirthDate devuelve hoy − 40 años.
func CivilMinBirthDate(today kernel.CivilDate) kernel.CivilDate {
	return kernel.CivilDateOf(today.Time().AddDate(-MaxPetAgeYears, 0, 0))
}

// Microchip es el identificador implantado (I2, DOM-004): ISO 11784/11785
// (15 dígitos) o legado (9/10 alfanuméricos). Normaliza quitando
// espacios/guiones y pasando a mayúsculas.
type Microchip string

var (
	microchipISORe   = regexp.MustCompile(`^\d{15}$`)
	microchipLegacyRe = regexp.MustCompile(`^[A-Z0-9]{9,10}$`)
)

// NewMicrochip valida y normaliza.
func NewMicrochip(s string) (Microchip, error) {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '-', '_':
			continue
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	v := b.String()
	if !microchipISORe.MatchString(v) && !microchipLegacyRe.MatchString(v) {
		return "", invalid("microchip")
	}
	return Microchip(v), nil
}

func (m Microchip) String() string { return string(m) }

// PhotoRef referencia la foto de la mascota en object storage: ObjectKey con
// prefijo `pets/<PetID>/` y contentType ∈ {jpeg, png, webp} (ARQ-012).
type PhotoRef struct {
	ObjectKey   string `json:"object_key"`
	ThumbKey    string `json:"thumb_key"`
	ContentType string `json:"content_type"`
}

var allowedPhotoContentTypes = map[string]bool{"jpeg": true, "png": true, "webp": true}

// NewPhotoRef valida la referencia.
func NewPhotoRef(petID uuid.UUID, objectKey, thumbKey, contentType string) (PhotoRef, error) {
	prefix := "pets/" + petID.String() + "/"
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if !strings.HasPrefix(objectKey, prefix) || !strings.HasPrefix(thumbKey, prefix) || !allowedPhotoContentTypes[ct] {
		return PhotoRef{}, invalid("photo")
	}
	return PhotoRef{ObjectKey: objectKey, ThumbKey: thumbKey, ContentType: ct}, nil
}

// CareLinkStatus es el estado de un vínculo de atención.
type CareLinkStatus string

// Estados admitidos.
const (
	LinkActive  CareLinkStatus = "active"
	LinkRevoked CareLinkStatus = "revoked"
)

// PetStatus es el estado vital de la mascota.
type PetStatus string

// Estados admitidos.
const (
	PetActive   PetStatus = "active"
	PetDeceased PetStatus = "deceased"
)

// invalid construye un ValidationError de un campo (422 VALIDATION_FAILED).
func invalid(field string) error {
	return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: field, Code: "INVALID"}}}
}
