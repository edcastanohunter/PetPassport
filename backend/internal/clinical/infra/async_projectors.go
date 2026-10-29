package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// WeightProjectorName es la clave del checkpoint (renombrar = rebuild).
const WeightProjectorName = app.ProjectionWeight

// CustodyProjectorName es la clave del checkpoint de la custodia clínica.
const CustodyProjectorName = app.ProjectionCustody

// WeightProjector alimenta read_clinical.weight_history con el peso de cada
// encuentro firmado (DOM-015: "EncounterSigned con vitals.weight_kg";
// PRD-025). Los borradores y descartados no cuentan, y un autoguardado
// repetido de constantes no duplica filas. Lee las constantes de
// read_clinical.encounters (proyección síncrona, siempre por delante de la
// asíncrona); el contenido firmado es inmutable, así que el rebuild produce
// el mismo resultado. Idempotente por (pet_id, ref_id = encounter_id).
type WeightProjector struct{}

var _ projection.AsyncProjector = WeightProjector{}

// NewWeightProjector construye el proyector.
func NewWeightProjector() WeightProjector { return WeightProjector{} }

// Name implementa projection.Projector.
func (WeightProjector) Name() string { return WeightProjectorName }

// Handles implementa projection.Projector.
func (WeightProjector) Handles() []string { return []string{"EncounterSigned"} }

// Project implementa projection.Projector.
func (WeightProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var p struct {
		EncounterID string `json:"encounter_id"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("weight: %s: %w", e.Type, err)
	}
	var petID, orgID string
	var measuredAt time.Time
	var weight *float64
	err := tx.QueryRow(ctx, `
		SELECT pet_id::text, organization_id::text, started_at, (vitals->>'weight_kg')::numeric::float8
		  FROM read_clinical.encounters WHERE id = $1`, p.EncounterID).
		Scan(&petID, &orgID, &measuredAt, &weight)
	if err != nil {
		return fmt.Errorf("weight: encuentro %s: %w", p.EncounterID, err)
	}
	if weight == nil {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO read_clinical.weight_history
		  (pet_id, measured_at, weight_kg, source, ref_id, organization_id)
		VALUES ($1, $2, $3, 'encounter', $4, $5)
		ON CONFLICT (pet_id, ref_id) DO NOTHING`,
		petID, measuredAt, *weight, p.EncounterID, orgID)
	if err != nil {
		return fmt.Errorf("weight: insertar: %w", err)
	}
	return nil
}

// Reset implementa projection.AsyncProjector.
func (WeightProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.weight_history`); err != nil {
		return fmt.Errorf("weight: reset: %w", err)
	}
	return nil
}

// CustodyProjector alimenta read_clinical.encounter_custody al firmar: la
// organización conserva lectura de lo que firmó aunque se revoque el vínculo
// (ADR-010, SEC-007). Idempotente por PK.
type CustodyProjector struct{}

var _ projection.AsyncProjector = CustodyProjector{}

// NewCustodyProjector construye el proyector.
func NewCustodyProjector() CustodyProjector { return CustodyProjector{} }

// Name implementa projection.Projector.
func (CustodyProjector) Name() string { return CustodyProjectorName }

// Handles implementa projection.Projector.
func (CustodyProjector) Handles() []string { return []string{"EncounterSigned"} }

// Project implementa projection.Projector.
func (CustodyProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var p struct {
		EncounterID    string `json:"encounter_id"`
		OrganizationID string `json:"organization_id"`
		PetID          string `json:"pet_id"`
		SignedAt       string `json:"signed_at"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("custody: %s: %w", e.Type, err)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.encounter_custody (organization_id, pet_id, encounter_id, signed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id, encounter_id) DO NOTHING`,
		p.OrganizationID, p.PetID, p.EncounterID, e.OccurredAt)
	if err != nil {
		return fmt.Errorf("custody: insertar: %w", err)
	}
	return nil
}

// Reset implementa projection.AsyncProjector.
func (CustodyProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.encounter_custody`); err != nil {
		return fmt.Errorf("custody: reset: %w", err)
	}
	return nil
}
