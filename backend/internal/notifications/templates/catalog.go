// Package templates renderiza los correos de Notifications (NOT-003): plantillas
// es/en versionadas, sin contenido clínico, con el pie común (motivo, gestionar
// preferencias, baja por categoría y dirección). Las plantillas viven en
// data/<nombre>/<locale>.{subject.txt,body.txt,body.html} y el catálogo de
// abajo es el manifiesto: nombre → versión y variables obligatorias.
package templates

import "sort"

// Spec describe una plantilla: versión (sube al cambiar el texto) y las
// variables que el llamador debe aportar. Un valor vacío es válido; una
// clave ausente hace fallar el render.
type Spec struct {
	Version int
	Vars    []string
}

// Catalog es el manifiesto de plantillas. Toda carpeta de data/ debe estar
// aquí y viceversa (TestManifestMatchesFiles).
var Catalog = map[string]Spec{
	"org_invitation":          {1, []string{"org_name", "role", "link"}},
	"transfer_invite":         {1, []string{"from_name", "pet_name", "expires_at", "link"}},
	"transfer_done":           {1, []string{"pet_name", "to_name", "link"}},
	"org_status":              {1, []string{"org_name", "status", "reason", "link"}},
	"care_link_request":       {1, []string{"org_name", "pet_name", "link"}},
	"care_link_result":        {1, []string{"org_name", "pet_name", "result", "link"}},
	"appointment_requested":   {1, []string{"pet_name", "type", "link"}},
	"appointment_confirmed":   {1, []string{"pet_name", "org_name", "when", "link"}},
	"appointment_reminder":    {1, []string{"pet_name", "org_name", "when", "link"}},
	"appointment_proposed":    {1, []string{"pet_name", "org_name", "when", "expires", "link"}},
	"appointment_rescheduled": {1, []string{"pet_name", "org_name", "when", "link"}},
	"appointment_cancelled":   {1, []string{"pet_name", "org_name", "when", "by", "link"}},
	"appointment_rejected":    {1, []string{"pet_name", "org_name", "link"}},
	"encounter_signed":        {1, []string{"pet_name", "org_name", "link"}},
	"encounter_addendum":      {1, []string{"pet_name", "org_name", "link"}},
	"follow_up_reminder":      {1, []string{"pet_name", "org_name", "due_date", "link"}},
	"vaccine_recorded":        {1, []string{"pet_name", "org_name", "link"}},
	"vaccine_reminder":        {1, []string{"pet_name", "due_date", "link"}},
	"clinic_suspended_notice": {1, []string{"org_name", "link"}},
	"clinic_daily_agenda":     {1, []string{"org_name", "date", "count", "link"}},
}

// Names devuelve los nombres del catálogo ordenados.
func Names() []string {
	out := make([]string, 0, len(Catalog))
	for n := range Catalog {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Locales soportados; el resto cae en es.
var Locales = []string{"es", "en"}
