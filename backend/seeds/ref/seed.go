// Package refseed aplica los catálogos de referencia de Clinical Records
// (ref.vaccines, ref.vaccination_protocols, ref.preventive_protocols;
// PP-F2-DAT-009) desde YAML versionado. Idempotente: ids deterministas.
//
// Mientras los archivos tengan `approved: false` los registros quedan con
// active = false (APPROVALS.md), salvo que se pida activar los no aprobados
// (solo desarrollo).
package refseed

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var files embed.FS

// Execer es el subconjunto de database/sql.DB, pgx.Tx y pgxpool.Pool que usa
// el cargador (se adapta con ExecFunc).
type Execer func(ctx context.Context, sql string, args ...any) error

// namespace fija los UUID v5 de los registros de referencia.
var namespace = uuid.MustParse("6f1d2c3a-9b8e-4f10-a5c7-0e2b4d6f8a10")

type vaccinesFile struct {
	Approved bool `yaml:"approved"`
	Vaccines []struct {
		Name     string   `yaml:"name"`
		Species  []string `yaml:"species"`
		Diseases []string `yaml:"diseases"`
		Type     string   `yaml:"type"`
		Routes   []string `yaml:"routes"`
	} `yaml:"vaccines"`
}

type seriesDose struct {
	MinAgeWeeks     int `yaml:"min_age_weeks" json:"min_age_weeks"`
	MinIntervalDays int `yaml:"min_interval_days" json:"min_interval_days"`
}

type protocolsFile struct {
	Approved  bool   `yaml:"approved"`
	Country   string `yaml:"country"`
	Protocols []struct {
		Vaccine               string       `yaml:"vaccine"`
		Species               string       `yaml:"species"`
		InitialSeries         []seriesDose `yaml:"initial_series"`
		BoosterIntervalMonths *int         `yaml:"booster_interval_months"`
		LegalValidityMonths   *int         `yaml:"legal_validity_months"`
		Notes                 string       `yaml:"notes"`
	} `yaml:"protocols"`
}

type preventiveFile struct {
	Approved  bool   `yaml:"approved"`
	Country   string `yaml:"country"`
	Protocols []struct {
		Species      string `yaml:"species"`
		Kind         string `yaml:"kind"`
		IntervalDays int    `yaml:"interval_days"`
		Notes        string `yaml:"notes"`
	} `yaml:"protocols"`
}

// VaccineID devuelve el id determinista de una vacuna del catálogo.
func VaccineID(name string) uuid.UUID { return uuid.NewSHA1(namespace, []byte("vaccine:"+name)) }

func load(name string, out any) error {
	raw, err := files.ReadFile(name)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("refseed %s: %w", name, err)
	}
	return nil
}

// Apply upserta los tres catálogos. includeUnapproved activa los registros de
// archivos con approved: false (solo dev). Devuelve cuántas filas escribió.
func Apply(ctx context.Context, exec Execer, includeUnapproved bool) (int, error) {
	var vf vaccinesFile
	var pf protocolsFile
	var xf preventiveFile
	if err := load("vaccines.yaml", &vf); err != nil {
		return 0, err
	}
	if err := load("protocols_CO.yaml", &pf); err != nil {
		return 0, err
	}
	if err := load("preventive_CO.yaml", &xf); err != nil {
		return 0, err
	}
	n := 0
	for _, v := range vf.Vaccines {
		if err := exec(ctx, `
			INSERT INTO ref.vaccines (id, name, species, diseases, type, routes, active)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, species=EXCLUDED.species, diseases=EXCLUDED.diseases,
			  type=EXCLUDED.type, routes=EXCLUDED.routes, active=EXCLUDED.active, updated_at=now()`,
			VaccineID(v.Name), v.Name, v.Species, v.Diseases, v.Type, v.Routes, vf.Approved || includeUnapproved); err != nil {
			return n, fmt.Errorf("refseed vaccine %q: %w", v.Name, err)
		}
		n++
	}
	for _, p := range pf.Protocols {
		series, err := json.Marshal(p.InitialSeries)
		if err != nil {
			return n, err
		}
		id := uuid.NewSHA1(namespace, []byte("protocol:"+pf.Country+":"+p.Species+":"+p.Vaccine))
		if err := exec(ctx, `
			INSERT INTO ref.vaccination_protocols
			  (id, country, species, vaccine_id, initial_series, booster_interval_months, legal_validity_months, notes, active)
			VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET initial_series=EXCLUDED.initial_series,
			  booster_interval_months=EXCLUDED.booster_interval_months, legal_validity_months=EXCLUDED.legal_validity_months,
			  notes=EXCLUDED.notes, active=EXCLUDED.active, updated_at=now()`,
			id, pf.Country, p.Species, VaccineID(p.Vaccine), series, p.BoosterIntervalMonths, p.LegalValidityMonths,
			p.Notes, pf.Approved || includeUnapproved); err != nil {
			return n, fmt.Errorf("refseed protocol %q/%s: %w", p.Vaccine, p.Species, err)
		}
		n++
	}
	for _, p := range xf.Protocols {
		id := uuid.NewSHA1(namespace, []byte(fmt.Sprintf("preventive:%s:%s:%s:%d", xf.Country, p.Species, p.Kind, p.IntervalDays)))
		if err := exec(ctx, `
			INSERT INTO ref.preventive_protocols (id, country, species, kind, interval_days, notes, active)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (id) DO UPDATE SET notes=EXCLUDED.notes, active=EXCLUDED.active`,
			id, xf.Country, p.Species, p.Kind, p.IntervalDays, p.Notes, xf.Approved || includeUnapproved); err != nil {
			return n, fmt.Errorf("refseed preventive %s/%s: %w", p.Species, p.Kind, err)
		}
		n++
	}
	return n, nil
}
