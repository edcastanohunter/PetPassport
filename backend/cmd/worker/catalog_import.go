// Package main provee el comando catalog-import para importar medicamentos veterinarios desde fuentes públicas (PP-F3-DAT-002, ADR-016).
// Fuente pública documentada: Instituto Colombiano Agropecuario (ICA) - Catálogo Oficial de Medicamentos Veterinarios de Colombia.
// Formato admitido: JSON o CSV con columnas [registro_ica, nombre, laboratorio, forma, concentracion, presentacion, especies, vias, principio_activo, requiere_receta, controlado].
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
	medicationsinfra "github.com/edcastanohunter/petpassport/backend/internal/medications/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

func catalogImportCommand(args []string) error {
	fs := flag.NewFlagSet("catalog-import", flag.ContinueOnError)
	source := fs.String("source", "ica", "Fuente del catálogo (p. ej. 'ica' para Colombia)")
	filePath := fs.String("file", "", "Ruta al archivo CSV o JSON a importar")
	dryRun := fs.Bool("dry-run", false, "Simula la importación sin persistir en la base de datos")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *filePath == "" {
		return errors.New("catalog-import: se requiere --file <ruta>")
	}

	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	pool, err := db.Open(context.Background(), cfg.DBURL, 5, nil)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pool.Close()

	store := medicationsinfra.NewStore(pool.Pool)

	data, err := os.ReadFile(*filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var meds []domain.Medication
	if strings.HasSuffix(strings.ToLower(*filePath), ".csv") {
		meds, err = parseCSVMedications(data, *source)
	} else {
		meds, err = parseJSONMedications(data, *source)
	}
	if err != nil {
		return fmt.Errorf("parse file: %w", err)
	}

	slog.Info("iniciando importación de catálogo", "source", *source, "file", *filePath, "rows", len(meds), "dry_run", *dryRun)

	batchID := uuid.Must(uuid.NewV7())
	adminID := uuid.MustParse("0192f3a1-7a10-7f0b-8c3e-0a1b2c3d4e00") // SuperAdmin system ID
	now := time.Now()

	batch := domain.ImportBatch{
		ID:        batchID,
		Source:    *source,
		Status:    "completed",
		DryRun:    *dryRun,
		CreatedBy: adminID,
		CreatedAt: now,
	}

	var importRows []domain.ImportRow

	for _, m := range meds {
		if m.Name == "" {
			batch.Skipped++
			batch.Errors = append(batch.Errors, "fila omitida: nombre vacío")
			continue
		}

		if *dryRun {
			batch.Created++
			continue
		}

		m.Source = domain.Source{
			Kind:       "import",
			Ref:        batchID.String(),
			ImportedAt: &now,
		}

		created, err := store.CreateMedication(context.Background(), adminID, m, fmt.Sprintf("importación lote %s (%s)", batchID, *source))
		if err != nil {
			batch.Skipped++
			batch.Errors = append(batch.Errors, fmt.Sprintf("%s: %v", m.Name, err))
		} else {
			batch.Created++
			importRows = append(importRows, domain.ImportRow{
				BatchID:      batchID,
				MedicationID: created.ID,
				Action:       "created",
			})
		}
	}

	if !*dryRun {
		if err := store.SaveImportBatch(context.Background(), batch, importRows); err != nil {
			return fmt.Errorf("save import batch: %w", err)
		}
	}

	slog.Info("importación de catálogo finalizada",
		"batch_id", batchID,
		"created", batch.Created,
		"updated", batch.Updated,
		"skipped", batch.Skipped,
		"errors_count", len(batch.Errors),
		"dry_run", *dryRun,
	)

	return nil
}

func parseJSONMedications(data []byte, source string) ([]domain.Medication, error) {
	var rawList []struct {
		RegistroICA     string   `json:"registro_ica"`
		Nombre          string   `json:"nombre"`
		Laboratorio     string   `json:"laboratorio"`
		Forma           string   `json:"forma"`
		Concentracion   string   `json:"concentracion"`
		Presentacion    string   `json:"presentacion"`
		Especies        []string `json:"especies"`
		Vias            []string `json:"vias"`
		PrincipioActivo string   `json:"principio_activo"`
		RequiereReceta  bool     `json:"requiere_receta"`
		Controlado      bool     `json:"controlado"`
	}

	if err := json.Unmarshal(data, &rawList); err != nil {
		// Intentar como { "rows": [...] }
		var wrapper struct {
			Rows json.RawMessage `json:"rows"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 == nil && len(wrapper.Rows) > 0 {
			if err3 := json.Unmarshal(wrapper.Rows, &rawList); err3 != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	var out []domain.Medication
	for _, r := range rawList {
		form := domain.FormTablet
		if r.Forma != "" {
			form = domain.Form(r.Forma)
		}
		reg := &domain.Regulatory{
			Country:        "CO",
			RegistryNumber: &r.RegistroICA,
			Authority:      "ICA",
		}
		var ings []domain.Ingredient
		if r.PrincipioActivo != "" {
			ings = append(ings, domain.Ingredient{Name: r.PrincipioActivo})
		}
		manu := r.Laboratorio
		conc := r.Concentracion
		pres := r.Presentacion

		out = append(out, domain.Medication{
			Name:                 r.Nombre,
			Form:                 form,
			Concentration:        &conc,
			Presentation:         &pres,
			Manufacturer:         &manu,
			Species:              r.Especies,
			Routes:               r.Vias,
			ActiveIngredients:    ings,
			PrescriptionRequired: r.RequiereReceta,
			Controlled:           r.Controlado,
			Regulatory:           reg,
			Status:               domain.StatusActive,
		})
	}
	return out, nil
}

func parseCSVMedications(data []byte, source string) ([]domain.Medication, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	header, err := r.Read()
	if err != nil {
		return nil, err
	}

	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	var out []domain.Medication
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		get := func(col string) string {
			if idx, ok := colIdx[col]; ok && idx < len(record) {
				return strings.TrimSpace(record[idx])
			}
			return ""
		}

		name := get("nombre")
		if name == "" {
			continue
		}

		regICA := get("registro_ica")
		lab := get("laboratorio")
		forma := get("forma")
		conc := get("concentracion")
		pres := get("presentacion")
		espStr := get("especies")
		viaStr := get("vias")
		actIng := get("principio_activo")
		reqReceta := strings.ToLower(get("requiere_receta")) == "true" || get("requiere_receta") == "1"
		controlado := strings.ToLower(get("controlado")) == "true" || get("controlado") == "1"

		var species []string
		if espStr != "" {
			for _, s := range strings.Split(espStr, ",") {
				if t := strings.TrimSpace(s); t != "" {
					species = append(species, t)
				}
			}
		}

		var routes []string
		if viaStr != "" {
			for _, v := range strings.Split(viaStr, ",") {
				if t := strings.TrimSpace(v); t != "" {
					routes = append(routes, t)
				}
			}
		}

		form := domain.Form(forma)
		if form == "" {
			form = domain.FormTablet
		}

		out = append(out, domain.Medication{
			Name:                 name,
			Form:                 form,
			Concentration:        &conc,
			Presentation:         &pres,
			Manufacturer:         &lab,
			Species:              species,
			Routes:               routes,
			ActiveIngredients:    []domain.Ingredient{{Name: actIng}},
			PrescriptionRequired: reqReceta,
			Controlled:           controlado,
			Regulatory: &domain.Regulatory{
				Country:        "CO",
				RegistryNumber: &regICA,
				Authority:      "ICA",
			},
			Status: domain.StatusActive,
		})
	}
	return out, nil
}
