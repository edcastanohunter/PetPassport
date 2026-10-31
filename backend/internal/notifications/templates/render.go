package templates

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

//go:embed data layout
var files embed.FS

// Errores del render.
var (
	ErrUnknownTemplate = errors.New("templates: plantilla desconocida")
	ErrMissingVar      = errors.New("templates: falta una variable obligatoria")
	ErrForeignLink     = errors.New("templates: el enlace no apunta a la base pública")
)

// Common son los datos del pie y del enlace, comunes a todas las plantillas.
type Common struct {
	BaseURL        string // PP_PUBLIC_BASE_URL: los enlaces solo se construyen contra ella (SEC-004)
	PreferencesURL string
	UnsubscribeURL string // vacío en security y en destinatarios sin cuenta
	Address        string // dirección física del remitente
	Category       string // define el motivo del envío
}

// Rendered es el correo listo para enviar.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
	Version int
	Locale  string
}

type view struct {
	Subject        string
	V              map[string]string
	Reason         string
	PreferencesURL string
	UnsubscribeURL string
	Address        string
}

// Render produce asunto, HTML y texto. Falla si la plantilla no existe, falta
// alguna variable del manifiesto o el enlace no cuelga de BaseURL.
func Render(name, locale string, vars map[string]string, c Common) (Rendered, error) {
	spec, ok := Catalog[name]
	if !ok {
		return Rendered{}, fmt.Errorf("%w: %s", ErrUnknownTemplate, name)
	}
	if locale != "en" {
		locale = "es"
	}
	for _, k := range spec.Vars {
		if _, ok := vars[k]; !ok {
			return Rendered{}, fmt.Errorf("%w: %s.%s", ErrMissingVar, name, k)
		}
	}
	if c.BaseURL != "" && !strings.HasPrefix(vars["link"], strings.TrimRight(c.BaseURL, "/")+"/") {
		return Rendered{}, fmt.Errorf("%w: %s", ErrForeignLink, name)
	}
	v := view{
		V: vars, Reason: reasonFor(c.Category, locale),
		PreferencesURL: c.PreferencesURL, UnsubscribeURL: c.UnsubscribeURL, Address: c.Address,
	}

	subject, err := renderText(name+"/"+locale+".subject.txt", "", &v)
	if err != nil {
		return Rendered{}, err
	}
	subject = strings.TrimSpace(subject)
	if strings.ContainsAny(subject, "\r\n") {
		return Rendered{}, fmt.Errorf("templates: asunto con salto de línea en %s", name)
	}
	v.Subject = subject

	text, err := renderText(name+"/"+locale+".body.txt", "layout/"+locale+".txt", &v)
	if err != nil {
		return Rendered{}, err
	}
	html, err := renderHTML(name+"/"+locale+".body.html", "layout/"+locale+".html", &v)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: subject, HTML: html, Text: text, Version: spec.Version, Locale: locale}, nil
}

func read(path string) (string, error) {
	b, err := files.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("templates: %w", err)
	}
	return string(b), nil
}

func renderText(bodyPath, layoutPath string, v *view) (string, error) {
	body, err := read("data/" + bodyPath)
	if err != nil {
		return "", err
	}
	t, err := texttemplate.New("body").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("templates: %s: %w", bodyPath, err)
	}
	name := "body"
	if layoutPath != "" {
		layout, err := read(layoutPath)
		if err != nil {
			return "", err
		}
		if t, err = t.New("layout").Parse(layout); err != nil {
			return "", fmt.Errorf("templates: %s: %w", layoutPath, err)
		}
		name = "layout"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, v); err != nil {
		return "", fmt.Errorf("templates: %s: %w", bodyPath, err)
	}
	return buf.String(), nil
}

func renderHTML(bodyPath, layoutPath string, v *view) (string, error) {
	body, err := read("data/" + bodyPath)
	if err != nil {
		return "", err
	}
	layout, err := read(layoutPath)
	if err != nil {
		return "", err
	}
	t, err := htmltemplate.New("body").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("templates: %s: %w", bodyPath, err)
	}
	if _, err = t.New("layout").Parse(layout); err != nil {
		return "", fmt.Errorf("templates: %s: %w", layoutPath, err)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		return "", fmt.Errorf("templates: %s: %w", bodyPath, err)
	}
	return buf.String(), nil
}

// reasonFor es el motivo del envío del pie (NOT-003).
func reasonFor(category, locale string) string {
	es := map[string]string{
		"appointments":    "Recibes este correo porque tienes citas o solicitudes de citas en PetPassport.",
		"preventive_care": "Recibes este correo porque tienes recordatorios de cuidado preventivo en PetPassport.",
		"clinical":        "Recibes este correo porque hay novedades en la historia clínica de tu mascota en PetPassport.",
		"account":         "Recibes este correo por tu cuenta o tu organización en PetPassport.",
		"security":        "Este aviso de seguridad no se puede desactivar.",
	}
	en := map[string]string{
		"appointments":    "You are receiving this email because you have appointments or appointment requests on PetPassport.",
		"preventive_care": "You are receiving this email because you have preventive care reminders on PetPassport.",
		"clinical":        "You are receiving this email because there is news in your pet's record on PetPassport.",
		"account":         "You are receiving this email because of your account or organization on PetPassport.",
		"security":        "This security notice cannot be turned off.",
	}
	m := es
	if locale == "en" {
		m = en
	}
	if s, ok := m[category]; ok {
		return s
	}
	return m["account"]
}
