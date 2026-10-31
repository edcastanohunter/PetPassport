package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// CardEntriesProjector proyecta read_clinical.card_entries y cards en la tx
// del comando (DAT-003, proyección síncrona). Idempotente: los inserts son
// ON CONFLICT DO NOTHING y las actualizaciones llevan guarda de versión.
type CardEntriesProjector struct{ reg *eventstore.Registry }

var (
	_              projection.Projector    = (*CardEntriesProjector)(nil)
	_              projection.SyncResetter = (*CardEntriesProjector)(nil)
	cardEventTypes                         = []string{
		"CardSpeciesSynced", "VaccinationEntryRecorded", "PreventiveEntryRecorded", "VaccinationEntryDeclared",
		"VaccinationEntryVerified", "VaccinationEntryDisputed", "VaccinationEntryVoided", "NextDueAdjusted",
	}
)

// NewCardEntriesProjector construye el proyector.
func NewCardEntriesProjector(reg *eventstore.Registry) *CardEntriesProjector {
	return &CardEntriesProjector{reg: reg}
}

// Name implementa Projector.
func (*CardEntriesProjector) Name() string { return "clinical.card_entries" }

// Handles implementa Projector.
func (*CardEntriesProjector) Handles() []string { return cardEventTypes }

// Project implementa Projector.
func (p *CardEntriesProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("card projector decode: %w", err)
	}
	switch ev := decoded.(type) {
	case domain.CardSpeciesSynced:
		_, err = tx.Exec(ctx, `
			INSERT INTO read_clinical.cards (pet_id, species, version, updated_at) VALUES ($1,$2,$3,$4)
			ON CONFLICT (pet_id) DO UPDATE SET species = EXCLUDED.species, version = EXCLUDED.version, updated_at = EXCLUDED.updated_at
			WHERE read_clinical.cards.version < EXCLUDED.version`,
			ev.PetID, ev.Species, e.StreamVersion, e.OccurredAt)
		return err

	case domain.VaccinationEntryRecorded:
		return insertRecorded(ctx, tx, e, ev.RecordedFields, domain.KindVaccine)
	case domain.PreventiveEntryRecorded:
		return insertRecorded(ctx, tx, e, ev.RecordedFields, ev.Kind)

	case domain.VaccinationEntryDeclared:
		var dueDate, dueBasis any
		if ev.NextDue != nil {
			dueDate, dueBasis = ev.NextDue.Date.String(), string(ev.NextDue.Basis)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO read_clinical.card_entries
			  (id, pet_id, kind, catalog_id, product_name, administered_at, lot, dose_label, source, verification,
			   declared_by, next_due_date, next_due_basis, status, evidence_object_key, version, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6::date,$7,$8,'declared','unverified',$9,$10::date,$11,'active',$12,$13,$14,$14)
			ON CONFLICT (id) DO NOTHING`,
			ev.EntryID, ev.PetID, string(ev.Kind), ev.Product.CatalogID, ev.Product.Name, ev.AdministeredAt.String(),
			ev.Lot, ev.DoseLabel, ev.DeclaredBy, dueDate, dueBasis, ev.EvidenceKey, e.StreamVersion, e.OccurredAt)
		return err

	case domain.VaccinationEntryVerified:
		return updateEntry(ctx, tx, e, ev.EntryID, `verification = 'verified'`)
	case domain.VaccinationEntryDisputed:
		return updateEntry(ctx, tx, e, ev.EntryID, `verification = 'disputed'`)
	case domain.VaccinationEntryVoided:
		_, err = tx.Exec(ctx, `UPDATE read_clinical.card_entries
			SET status = 'voided', void_reason = $3, version = $2, updated_at = $4
			WHERE id = $1 AND version < $2`, ev.EntryID, e.StreamVersion, ev.Reason, e.OccurredAt)
		return err
	case domain.NextDueAdjusted:
		_, err = tx.Exec(ctx, `UPDATE read_clinical.card_entries
			SET next_due_date = $3::date, next_due_basis = $4, version = $2, updated_at = $5
			WHERE id = $1 AND version < $2`, ev.EntryID, e.StreamVersion, ev.Date.String(), string(ev.Basis), e.OccurredAt)
		return err
	}
	return nil
}

func updateEntry(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, id uuid.UUID, set string) error {
	_, err := tx.Exec(ctx, `UPDATE read_clinical.card_entries SET `+set+`, version = $2, updated_at = $3
		WHERE id = $1 AND version < $2`, id, e.StreamVersion, e.OccurredAt)
	return err
}

func insertRecorded(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, r domain.RecordedFields, kind domain.EntryKind) error {
	var dueDate, dueBasis, expiry any
	if r.NextDue != nil {
		dueDate, dueBasis = r.NextDue.Date.String(), string(r.NextDue.Basis)
	}
	if r.ExpiryDate != nil {
		expiry = r.ExpiryDate.String()
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.card_entries
		  (id, pet_id, kind, catalog_id, product_name, administered_at, lot, manufacturer, expiry_date, route, site,
		   dose_label, weight_kg, source, verification, recorded_by_user, recorded_by_org, recorded_at, encounter_id,
		   next_due_date, next_due_basis, status, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::date,$7,$8,$9::date,$10,$11,$12,$13,$14,'verified',$15,$16,$17,$18,$19::date,$20,
		        'active',$21,$22,$22)
		ON CONFLICT (id) DO NOTHING`,
		r.EntryID, r.PetID, string(kind), r.Product.CatalogID, r.Product.Name, r.AdministeredAt.String(), r.Lot,
		r.Manufacturer, expiry, r.Route, r.Site, r.DoseLabel, r.WeightKg, string(r.Source),
		r.RecordedBy.UserID, r.RecordedBy.OrganizationID, r.RecordedAt, r.EncounterID, dueDate, dueBasis,
		e.StreamVersion, e.OccurredAt)
	return err
}

// ResetForRebuild implementa projection.SyncResetter.
func (*CardEntriesProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	for _, t := range []string{"card_entries", "cards"} {
		if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.`+t); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// VaccinationDueProjectorName es la clave del checkpoint (renombrar = rebuild).
const VaccinationDueProjectorName = app.ProjectionVaccinationDue

// VaccinationDueProjector alimenta read_clinical.vaccination_due (ARQ-012,
// asíncrona). Recalcula las filas de la mascota a partir de card_entries (la
// proyección síncrona va siempre por delante): idempotente y sin estado
// propio. Estados: upcoming (el vencimiento se deriva en consulta por fecha),
// satisfied (hay una dosis posterior del mismo producto) y cancelled (entrada
// anulada).
type VaccinationDueProjector struct{}

var _ projection.AsyncProjector = VaccinationDueProjector{}

// NewVaccinationDueProjector construye el proyector.
func NewVaccinationDueProjector() VaccinationDueProjector { return VaccinationDueProjector{} }

// Name implementa projection.Projector.
func (VaccinationDueProjector) Name() string { return VaccinationDueProjectorName }

// Handles implementa projection.Projector.
func (VaccinationDueProjector) Handles() []string {
	return []string{
		"VaccinationEntryRecorded", "PreventiveEntryRecorded", "VaccinationEntryDeclared",
		"VaccinationEntryVoided", "NextDueAdjusted",
	}
}

// Project implementa projection.Projector.
func (VaccinationDueProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var p struct {
		PetID string `json:"pet_id"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("vaccination_due: %s: %w", e.Type, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.vaccination_due WHERE pet_id = $1`, p.PetID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.vaccination_due (pet_id, entry_id, kind, product_name, due_date, basis, status)
		SELECT c.pet_id, c.id, c.kind, c.product_name, c.next_due_date, c.next_due_basis,
		       CASE WHEN c.status = 'voided' THEN 'cancelled'
		            WHEN EXISTS (SELECT 1 FROM read_clinical.card_entries n
		                          WHERE n.pet_id = c.pet_id AND n.kind = c.kind AND n.status = 'active'
		                            AND lower(n.product_name) = lower(c.product_name)
		                            AND n.administered_at > c.administered_at) THEN 'satisfied'
		            ELSE 'upcoming' END
		  FROM read_clinical.card_entries c
		 WHERE c.pet_id = $1 AND c.next_due_date IS NOT NULL`, p.PetID)
	return err
}

// Reset implementa projection.AsyncProjector.
func (VaccinationDueProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.vaccination_due`); err != nil {
		return fmt.Errorf("vaccination_due: reset: %w", err)
	}
	return nil
}
