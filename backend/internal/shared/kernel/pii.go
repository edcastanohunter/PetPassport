package kernel

import (
	"errors"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

// Value objects comunes con datos personales. Implementan slog.LogValuer para
// que nunca aparezcan en claro en los logs (docs/phase-1/08 §8 SEC-007).

// ErrInvalidEmail indica un email mal formado.
var ErrInvalidEmail = errors.New("invalid email")

// ErrInvalidPhone indica un teléfono que no es E.164.
var ErrInvalidPhone = errors.New("invalid phone")

// ErrInvalidCountry indica un código de país que no es ISO 3166-1 alpha-2.
var ErrInvalidCountry = errors.New("invalid country")

// Email es una dirección normalizada (minúsculas, sin espacios).
type Email string

// NewEmail valida y normaliza.
func NewEmail(s string) (Email, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 254 {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return "", ErrInvalidEmail
	}
	return Email(s), nil
}

func (e Email) String() string { return string(e) }

// Masked devuelve a***@dominio para mostrar sin revelar la dirección.
func (e Email) Masked() string {
	s := string(e)
	at := strings.IndexByte(s, '@')
	if at <= 0 {
		return "***"
	}
	return s[:1] + "***" + s[at:]
}

// LogValue enmascara el email en logs.
func (e Email) LogValue() slog.Value { return slog.StringValue(e.Masked()) }

var phoneRe = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// Phone es un número en formato E.164.
type Phone string

// NewPhone valida y normaliza (quita espacios, guiones y paréntesis).
func NewPhone(s string) (Phone, error) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r == '+' || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '(' || r == ')' || r == '.' {
			continue
		} else {
			return "", ErrInvalidPhone
		}
	}
	p := b.String()
	if !phoneRe.MatchString(p) {
		return "", ErrInvalidPhone
	}
	return Phone(p), nil
}

func (p Phone) String() string { return string(p) }

// Masked conserva prefijo y últimos dos dígitos.
func (p Phone) Masked() string {
	s := string(p)
	if len(s) < 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-2:]
}

// LogValue enmascara el teléfono en logs.
func (p Phone) LogValue() slog.Value { return slog.StringValue(p.Masked()) }

// CountryCode es ISO 3166-1 alpha-2 en mayúsculas.
type CountryCode string

// NewCountryCode valida el formato (no la existencia del país).
func NewCountryCode(s string) (CountryCode, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 2 || s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
		return "", ErrInvalidCountry
	}
	return CountryCode(s), nil
}

func (c CountryCode) String() string { return string(c) }

// Locale es el idioma de la interfaz.
type Locale string

const (
	LocaleES Locale = "es"
	LocaleEN Locale = "en"
)

// NewLocale valida contra los idiomas soportados.
func NewLocale(s string) (Locale, error) {
	switch Locale(strings.ToLower(strings.TrimSpace(s))) {
	case LocaleES:
		return LocaleES, nil
	case LocaleEN:
		return LocaleEN, nil
	}
	return "", errors.New("unsupported locale")
}

// Address es una dirección postal (docs/phase-1/02 §4.2).
type Address struct {
	Country    CountryCode `json:"country"`
	Region     string      `json:"region,omitempty"`
	City       string      `json:"city"`
	Line1      string      `json:"line1"`
	Line2      string      `json:"line2,omitempty"`
	PostalCode string      `json:"postal_code,omitempty"`
}

// LogValue no expone la dirección.
func (Address) LogValue() slog.Value { return slog.StringValue("[address]") }

// Contact es un contacto de emergencia.
type Contact struct {
	Name         string `json:"name"`
	Phone        Phone  `json:"phone"`
	Relationship string `json:"relationship,omitempty"`
}

// LogValue no expone el contacto.
func (Contact) LogValue() slog.Value { return slog.StringValue("[contact]") }
