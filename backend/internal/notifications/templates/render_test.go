package templates_test

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/templates"
)

var update = flag.Bool("update", false, "reescribe los golden de las plantillas")

const base = "https://app.example"

// sample rellena todas las variables del manifiesto con valores de prueba.
func sample(name string) map[string]string {
	v := map[string]string{}
	for _, k := range templates.Catalog[name].Vars {
		v[k] = "valor-" + k
	}
	v["link"] = base + "/app/x"
	switch name {
	case "org_status":
		v["status"] = "rejected"
		v["reason"] = "RUT vencido"
	case "care_link_result":
		v["result"] = "approved"
	case "appointment_cancelled":
		v["by"] = "guardian"
		v["when"] = "lunes 2 nov 09:00"
	}
	return v
}

func common() templates.Common {
	return templates.Common{
		BaseURL: base, PreferencesURL: base + "/app/settings/notifications",
		UnsubscribeURL: base + "/unsubscribe/tok", Address: "PetPassport · Dirección de prueba", Category: "appointments",
	}
}

// QA-005: snapshot de cada plantilla en es/en (asunto, HTML y texto).
func TestGolden(t *testing.T) {
	for _, name := range templates.Names() {
		for _, loc := range templates.Locales {
			t.Run(name+"/"+loc, func(t *testing.T) {
				r, err := templates.Render(name, loc, sample(name), common())
				if err != nil {
					t.Fatal(err)
				}
				got := "SUBJECT: " + r.Subject + "\nVERSION: " + strconv.Itoa(r.Version) + "\n--- TEXT ---\n" + r.Text + "--- HTML ---\n" + r.HTML
				path := filepath.Join("testdata", name+"."+loc+".golden")
				if *update {
					if err := os.MkdirAll("testdata", 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path) //nolint:gosec // G304: golden bajo testdata/ con nombre de plantilla del catálogo
				if err != nil {
					t.Fatalf("falta el golden (go test -update): %v", err)
				}
				if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
					t.Fatalf("snapshot distinto en %s; si el cambio es intencional sube la versión y ejecuta go test -update", path)
				}
			})
		}
	}
}

// El manifiesto y los archivos embebidos coinciden.
func TestManifestMatchesFiles(t *testing.T) {
	dirs, err := os.ReadDir("data")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		seen[d.Name()] = true
		if _, ok := templates.Catalog[d.Name()]; !ok {
			t.Errorf("data/%s no está en el manifiesto", d.Name())
		}
		for _, loc := range templates.Locales {
			for _, ext := range []string{".subject.txt", ".body.txt", ".body.html"} {
				if _, err := fs.Stat(os.DirFS("data/"+d.Name()), loc+ext); err != nil {
					t.Errorf("%s: falta %s%s", d.Name(), loc, ext)
				}
			}
		}
	}
	for n := range templates.Catalog {
		if !seen[n] {
			t.Errorf("%s está en el manifiesto pero no tiene archivos", n)
		}
	}
}

// Render falla si falta una variable del manifiesto (NOT-003).
func TestMissingVariableFails(t *testing.T) {
	for _, name := range templates.Names() {
		for _, k := range templates.Catalog[name].Vars {
			v := sample(name)
			delete(v, k)
			if _, err := templates.Render(name, "es", v, common()); !errors.Is(err, templates.ErrMissingVar) {
				t.Errorf("%s sin %s: esperaba ErrMissingVar, obtuvo %v", name, k, err)
			}
		}
	}
}

func TestUnknownTemplateAndLocaleFallback(t *testing.T) {
	if _, err := templates.Render("no_existe", "es", nil, common()); !errors.Is(err, templates.ErrUnknownTemplate) {
		t.Fatalf("err: %v", err)
	}
	r, err := templates.Render("transfer_done", "fr", sample("transfer_done"), common())
	if err != nil || r.Locale != "es" {
		t.Fatalf("un locale desconocido cae en es: %+v %v", r, err)
	}
}

// SEC-004: el enlace solo puede colgar de la base pública configurada.
func TestForeignLinkRejected(t *testing.T) {
	v := sample("transfer_done")
	v["link"] = "https://evil.example/app"
	if _, err := templates.Render("transfer_done", "es", v, common()); !errors.Is(err, templates.ErrForeignLink) {
		t.Fatalf("err: %v", err)
	}
	v["link"] = base + ".evil.example/app" // prefijo engañoso sin barra
	if _, err := templates.Render("transfer_done", "es", v, common()); !errors.Is(err, templates.ErrForeignLink) {
		t.Fatalf("err: %v", err)
	}
}

// El HTML escapa lo que viene de usuarios; el asunto no admite saltos de línea.
func TestEscapingAndSubject(t *testing.T) {
	v := sample("encounter_signed")
	v["pet_name"] = `<script>alert(1)</script>`
	r, err := templates.Render("encounter_signed", "es", v, common())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<script>") || !strings.Contains(r.HTML, "&lt;script&gt;") {
		t.Fatalf("html sin escapar: %s", r.HTML)
	}
	v["pet_name"] = "Luna\r\nBcc: x@y.z"
	if _, err := templates.Render("encounter_signed", "es", v, common()); err == nil {
		t.Fatal("un salto de línea en el asunto debe fallar")
	}
}

// Pie común (NOT-003): motivo, preferencias, baja y dirección; security sin baja.
func TestFooter(t *testing.T) {
	r, err := templates.Render("appointment_confirmed", "en", sample("appointment_confirmed"), common())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/app/settings/notifications", "/unsubscribe/tok", "Dirección de prueba", "appointments"} {
		if want == "appointments" {
			want = "appointments or appointment requests"
		}
		if !strings.Contains(r.Text, want) || !strings.Contains(r.HTML, want) {
			t.Errorf("el pie no contiene %q", want)
		}
	}
	c := common()
	c.UnsubscribeURL = ""
	c.Category = "security"
	r, _ = templates.Render("appointment_confirmed", "es", sample("appointment_confirmed"), c)
	if strings.Contains(r.Text, "Dejar de recibir") || !strings.Contains(r.Text, "no se puede desactivar") {
		t.Fatalf("security no ofrece baja: %s", r.Text)
	}
}
