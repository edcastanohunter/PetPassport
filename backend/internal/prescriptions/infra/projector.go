package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PrescriptionsProjector mantiene de forma síncrona el read model read_prescriptions (PP-F4-DAT-001, ARQ-014).
// Es idempotente ante reproyecciones.
type PrescriptionsProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*PrescriptionsProjector)(nil)

// NewPrescriptionsProjector construye el proyector.
func NewPrescriptionsProjector(reg *eventstore.Registry) *PrescriptionsProjector {
	return &PrescriptionsProjector{reg: reg}
}

func (*PrescriptionsProjector) Name() string { return "prescriptions.read_prescriptions" }

func (*PrescriptionsProjector) Handles() []string {
	return []string{
		"PrescriptionDrafted",
		"PrescriptionDraftUpdated",
		"PrescriptionIssued",
		"PrescriptionRevoked",
		"ExternalPrescriptionUploaded",
		"PrescriptionExtractionRecorded",
		"PrescriptionReuploadRequested",
		"ExternalPrescriptionConfirmed",
		"ExternalPrescriptionReopened",
		"PrescriptionPharmacistValidated",
		"PrescriptionDispensed",
		"PrescriptionExhausted",
		"PrescriptionExpired",
	}
}

func jsonBytes(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

func (p *PrescriptionsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("prescriptions projector decode: %w", err)
	}

	switch ev := decoded.(type) {
	case domain.PrescriptionDrafted:
		itemsJSON, err := jsonBytes(ev.Items)
		if err != nil {
			return err
		}
		remMap := make(map[string]float64)
		hasControlled := false
		for _, it := range ev.Items {
			remMap[it.ItemID] = it.QuantityToDispense.Value
			if it.Controlled {
				hasControlled = true
			}
		}
		remJSON, err := jsonBytes(remMap)
		if err != nil {
			return err
		}

		validUntil := ev.Validity.ValidUntil.Time()

		const insertDraft = `
INSERT INTO read_prescriptions.prescriptions (
  id, version, pet_id, guardian_id, country, source, status,
  issuer_id, issuer_org_id, encounter_id, valid_until, refills_allowed,
  items, items_remaining, has_controlled, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
ON CONFLICT (id) DO UPDATE SET
  version = EXCLUDED.version,
  items = EXCLUDED.items,
  items_remaining = EXCLUDED.items_remaining,
  valid_until = EXCLUDED.valid_until,
  refills_allowed = EXCLUDED.refills_allowed,
  has_controlled = EXCLUDED.has_controlled,
  updated_at = EXCLUDED.updated_at
WHERE read_prescriptions.prescriptions.version <= EXCLUDED.version`

		_, err = tx.Exec(ctx, insertDraft,
			ev.PrescriptionID, e.StreamVersion, ev.PetID, ev.GuardianID, ev.Country, string(ev.Source), string(domain.StatusDraft),
			ev.IssuerID, ev.IssuerOrgID, ev.EncounterID, validUntil, ev.Validity.RefillsAllowed,
			itemsJSON, remJSON, hasControlled, e.OccurredAt,
		)
		return err

	case domain.PrescriptionDraftUpdated:
		itemsJSON, err := jsonBytes(ev.Items)
		if err != nil {
			return err
		}
		remMap := make(map[string]float64)
		hasControlled := false
		for _, it := range ev.Items {
			remMap[it.ItemID] = it.QuantityToDispense.Value
			if it.Controlled {
				hasControlled = true
			}
		}
		remJSON, err := jsonBytes(remMap)
		if err != nil {
			return err
		}
		validUntil := ev.Validity.ValidUntil.Time()

		const updateDraft = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, items = $3, items_remaining = $4, valid_until = $5,
       refills_allowed = $6, has_controlled = $7, updated_at = $8
 WHERE id = $1 AND version <= $2`

		_, err = tx.Exec(ctx, updateDraft,
			ev.PrescriptionID, e.StreamVersion, itemsJSON, remJSON, validUntil,
			ev.Validity.RefillsAllowed, hasControlled, e.OccurredAt,
		)
		return err

	case domain.PrescriptionIssued:
		credJSON, err := jsonBytes(ev.Credential)
		if err != nil {
			return err
		}
		sig := domain.Signature{
			SignedBy:      ev.IssuerID,
			SignedAt:      ev.IssuedAt,
			StreamVersion: ev.StreamVersion,
			ContentHash:   ev.ContentHash,
		}
		sigJSON, err := jsonBytes(sig)
		if err != nil {
			return err
		}
		itemsJSON, err := jsonBytes(ev.Items)
		if err != nil {
			return err
		}
		hasControlled := false
		for _, it := range ev.Items {
			if it.Controlled {
				hasControlled = true
			}
		}
		validUntil := ev.ValidUntil.Time()

		const updateIssued = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, issuer_id = $4, credential = $5, issued_at = $6,
       valid_until = $7, refills_allowed = $8, items = $9, signature = $10,
       has_controlled = $11, updated_at = $12
 WHERE id = $1 AND version <= $2`

		_, err = tx.Exec(ctx, updateIssued,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusIssued), ev.IssuerID,
			credJSON, ev.IssuedAt, validUntil, ev.RefillsAllowed, itemsJSON, sigJSON,
			hasControlled, e.OccurredAt,
		)
		return err

	case domain.PrescriptionRevoked:
		const updateRevoked = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, updated_at = $4
 WHERE id = $1 AND version <= $2`

		_, err = tx.Exec(ctx, updateRevoked,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusRevoked), e.OccurredAt,
		)
		return err

	case domain.ExternalPrescriptionUploaded:
		const insertExt = `
INSERT INTO read_prescriptions.prescriptions (
  id, version, pet_id, guardian_id, country, source, status,
  created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (id) DO UPDATE SET
  version = EXCLUDED.version,
  updated_at = EXCLUDED.updated_at
WHERE read_prescriptions.prescriptions.version <= EXCLUDED.version`

		_, err = tx.Exec(ctx, insertExt,
			ev.PrescriptionID, e.StreamVersion, ev.PetID, ev.GuardianID, ev.Country,
			string(domain.SourceExternal), string(domain.StatusDraft), e.OccurredAt,
		)
		if err != nil {
			return err
		}

		for _, img := range ev.Images {
			const insertImg = `
INSERT INTO read_prescriptions.images (
  prescription_id, page, object_key, content_type, width, height, sha256, scan_status, uploaded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8)
ON CONFLICT (prescription_id, page) DO NOTHING`
			_, err = tx.Exec(ctx, insertImg,
				ev.PrescriptionID, img.Page, img.ObjectKey, img.ContentType, img.Width, img.Height, img.Sha256, e.OccurredAt,
			)
			if err != nil {
				return err
			}
		}

		const insertInitialJob = `
INSERT INTO read_prescriptions.extraction_jobs (
  prescription_id, attempt, status, created_at, updated_at
) VALUES ($1, 1, 'queued', $2, $2)
ON CONFLICT (prescription_id, attempt) DO NOTHING`
		_, _ = tx.Exec(ctx, insertInitialJob, ev.PrescriptionID, e.OccurredAt)
		return nil

	case domain.PrescriptionExtractionRecorded:
		warningsJSON, _ := jsonBytes(ev.Extraction.Warnings)
		const upsertDoneJob = `
INSERT INTO read_prescriptions.extraction_jobs (
  prescription_id, attempt, status, extractor_id, legibility, warnings,
  cost_usd, latency_ms, created_at, updated_at
) VALUES ($1, $2, 'done', $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (prescription_id, attempt) DO UPDATE SET
  status = 'done',
  extractor_id = EXCLUDED.extractor_id,
  legibility = EXCLUDED.legibility,
  warnings = EXCLUDED.warnings,
  cost_usd = EXCLUDED.cost_usd,
  latency_ms = EXCLUDED.latency_ms,
  updated_at = EXCLUDED.updated_at`
		_, err = tx.Exec(ctx, upsertDoneJob,
			ev.PrescriptionID, ev.Extraction.Version, ev.Extraction.ExtractorID,
			ev.Extraction.Legibility, warningsJSON, ev.Extraction.CostUSD,
			ev.Extraction.LatencyMs, e.OccurredAt,
		)
		return err

	case domain.PrescriptionReuploadRequested:
		const upsertReuploadJob = `
INSERT INTO read_prescriptions.extraction_jobs (
  prescription_id, attempt, status, error, created_at, updated_at
) VALUES ($1, 1, 'reupload_requested', $2, $3, $3)
ON CONFLICT (prescription_id, attempt) DO UPDATE SET
  status = 'reupload_requested',
  error = EXCLUDED.error,
  updated_at = EXCLUDED.updated_at`
		_, err = tx.Exec(ctx, upsertReuploadJob, ev.PrescriptionID, ev.Reason, e.OccurredAt)
		return err

	case domain.ExternalPrescriptionConfirmed:
		itemsJSON, err := jsonBytes(ev.Items)
		if err != nil {
			return err
		}
		metaJSON, err := jsonBytes(ev.ExternalMeta)
		if err != nil {
			return err
		}
		conf := domain.Confirmation{
			ConfirmedAt: e.OccurredAt,
			Diff:        ev.Diff,
		}
		confJSON, err := jsonBytes(conf)
		if err != nil {
			return err
		}
		remMap := make(map[string]float64)
		hasControlled := false
		for _, it := range ev.Items {
			remMap[it.ItemID] = it.QuantityToDispense.Value
			if it.Controlled {
				hasControlled = true
			}
		}
		remJSON, err := jsonBytes(remMap)
		if err != nil {
			return err
		}
		validUntil := ev.Validity.ValidUntil.Time()

		const updateConfirmed = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, items = $4, items_remaining = $5, valid_until = $6,
       refills_allowed = $7, external_meta = $8, confirmation = $9, has_controlled = $10, updated_at = $11
 WHERE id = $1 AND version <= $2`

		_, err = tx.Exec(ctx, updateConfirmed,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusConfirmed), itemsJSON, remJSON,
			validUntil, ev.Validity.RefillsAllowed, metaJSON, confJSON, hasControlled, e.OccurredAt,
		)
		return err

	case domain.ExternalPrescriptionReopened:
		const updateReopen = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, updated_at = $4
 WHERE id = $1 AND version <= $2`
		_, err = tx.Exec(ctx, updateReopen,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusDraft), e.OccurredAt,
		)
		return err

	case domain.PrescriptionPharmacistValidated:
		val := domain.PharmacistValidation{
			By:     ev.By,
			OrgID:  ev.OrgID,
			Status: ev.Status,
			Note:   ev.Note,
			At:     e.OccurredAt,
		}
		valJSON, err := jsonBytes(val)
		if err != nil {
			return err
		}

		const updateVal = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, validation = $3, updated_at = $4
 WHERE id = $1 AND version <= $2`
		_, err = tx.Exec(ctx, updateVal,
			ev.PrescriptionID, e.StreamVersion, valJSON, e.OccurredAt,
		)
		return err

	case domain.PrescriptionDispensed:
		linesJSON, err := jsonBytes(ev.Lines)
		if err != nil {
			return err
		}

		dispID := kernel.NewID()
		const insertDisp = `
INSERT INTO read_prescriptions.dispensations (
  id, prescription_id, order_id, pharmacy_org_id, at, lines, partial
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING`
		_, err = tx.Exec(ctx, insertDisp,
			dispID, ev.PrescriptionID, ev.OrderID, ev.PharmacyOrgID, e.OccurredAt, linesJSON, ev.Partial,
		)
		if err != nil {
			return err
		}

		// Descontar saldos de items_remaining
		var currentRemJSON []byte
		var itemsJSON []byte
		var currentRefillsUsed int
		var refillsAllowed int
		err = tx.QueryRow(ctx, `
			SELECT items_remaining, items, refills_used, refills_allowed
			  FROM read_prescriptions.prescriptions
			 WHERE id = $1`, ev.PrescriptionID).Scan(&currentRemJSON, &itemsJSON, &currentRefillsUsed, &refillsAllowed)
		if err != nil {
			return err
		}

		remMap := make(map[string]float64)
		if len(currentRemJSON) > 0 {
			_ = json.Unmarshal(currentRemJSON, &remMap)
		}
		for _, l := range ev.Lines {
			remMap[l.ItemID] -= l.Qty
			if remMap[l.ItemID] < 0 {
				remMap[l.ItemID] = 0
			}
		}

		// Si se usó un nuevo refill, restaurar items al total prescrito si no se ha agotado
		if ev.RefillsUsed > currentRefillsUsed && ev.RefillsUsed <= refillsAllowed {
			var origItems []domain.PrescriptionItem
			if len(itemsJSON) > 0 {
				_ = json.Unmarshal(itemsJSON, &origItems)
				for _, it := range origItems {
					remMap[it.ItemID] = it.QuantityToDispense.Value
				}
			}
		}

		newRemJSON, err := jsonBytes(remMap)
		if err != nil {
			return err
		}

		const updateDisp = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, items_remaining = $3, refills_used = $4, updated_at = $5
 WHERE id = $1 AND version <= $2`
		_, err = tx.Exec(ctx, updateDisp,
			ev.PrescriptionID, e.StreamVersion, newRemJSON, ev.RefillsUsed, e.OccurredAt,
		)
		return err

	case domain.PrescriptionExhausted:
		const updateExhausted = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, updated_at = $4
 WHERE id = $1 AND version <= $2`
		_, err = tx.Exec(ctx, updateExhausted,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusExhausted), e.OccurredAt,
		)
		return err

	case domain.PrescriptionExpired:
		const updateExpired = `
UPDATE read_prescriptions.prescriptions
   SET version = $2, status = $3, updated_at = $4
 WHERE id = $1 AND version <= $2`
		_, err = tx.Exec(ctx, updateExpired,
			ev.PrescriptionID, e.StreamVersion, string(domain.StatusExpired), e.OccurredAt,
		)
		return err
	}

	return nil
}

// RegisterSyncProjectors registra las proyecciones síncronas de prescripciones en el Runner.
func RegisterSyncProjectors(runner *projection.Runner, reg *eventstore.Registry) {
	runner.Register(NewPrescriptionsProjector(reg))
}

// SyncProjector adapta el runner de proyecciones para satisfacer app.ProjectionRunner.
type SyncProjector struct {
	runner *projection.Runner
}

// NewSyncProjector construye el adaptador.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply aplica los proyectores en la tx del contexto.
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return a.runner.Apply(ctx, tx, events)
}
