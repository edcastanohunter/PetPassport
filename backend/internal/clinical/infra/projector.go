package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// DocumentJobKindEncounterPDF identifica el trabajo de PDF de un encuentro.
const DocumentJobKindEncounterPDF = app.DocumentJobKindEncounterPDF

// EncountersProjector proyecta read_clinical.encounters y
// encounter_attachments en la tx del comando (DAT-002, proyección síncrona).
// Idempotente por guarda de versión: reproyectar no cambia el resultado.
type EncountersProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*EncountersProjector)(nil)

// NewEncountersProjector construye el proyector.
func NewEncountersProjector(reg *eventstore.Registry) *EncountersProjector {
	return &EncountersProjector{reg: reg}
}

// Name implementa Projector.
func (*EncountersProjector) Name() string { return "clinical.encounters" }

// Handles implementa Projector.
func (*EncountersProjector) Handles() []string {
	return []string{
		"EncounterOpened", "EncounterDetailsUpdated", "VitalsRecorded",
		"EncounterNarrativeUpdated", "DiagnosesSet", "PlanSet", "FollowUpRecommended",
		"FollowUpCleared", "AttendingVetAssigned", "AttachmentAdded", "AttachmentRemoved",
		"AttachmentVisibilityChanged", "EncounterSigned", "EncounterAddendumAdded",
		"EncounterDiscarded",
	}
}

// JSON helpers: el read model guarda JSONB y la API lo devuelve tal cual.
func jsonArg(v any) ([]byte, error) { return json.Marshal(v) }

// Project implementa Projector.
func (p *EncountersProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("clinical projector decode: %w", err)
	}
	switch ev := decoded.(type) {
	case domain.EncounterOpened:
		plan, err := jsonArg(emptyPlan())
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO read_clinical.encounters
			  (id, version, pet_id, organization_id, appointment_id, type, status, started_at, reason,
			   attending_vet_id, diagnoses, plan, addenda, last_activity_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,'draft',$7,$8,$9,'[]'::jsonb,$10::jsonb,'[]'::jsonb,$11,$11,$11)
			ON CONFLICT (id) DO NOTHING`,
			ev.EncounterID, e.StreamVersion, ev.PetID, ev.OrganizationID, ev.AppointmentID,
			string(ev.Type), ev.StartedAt, ev.Reason, ev.AttendingVetID, plan, e.OccurredAt)
		return err

	case domain.EncounterDetailsUpdated:
		_, err := tx.Exec(ctx, `
			UPDATE read_clinical.encounters SET
			  version = $2, updated_at = $3, last_activity_at = $3,
			  type = COALESCE($4, type),
			  reason = COALESCE($5, reason),
			  started_at = COALESCE($6, started_at)
			WHERE id = $1 AND version < $2`,
			ev.EncounterID, e.StreamVersion, e.OccurredAt, ev.Type, ev.Reason, ev.StartedAt)
		return err

	case domain.VitalsRecorded:
		vitals, err := jsonArg(ev.Vitals)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, vitals = $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, vitals)
		return err

	case domain.EncounterNarrativeUpdated:
		var objective any
		if ev.Objective != nil {
			raw, err := jsonArg(ev.Objective)
			if err != nil {
				return err
			}
			objective = raw
		}
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters SET
			version = $2, updated_at = $3, last_activity_at = $3,
			subjective = CASE WHEN $4::boolean THEN $5 ELSE subjective END,
			objective = CASE WHEN $4::boolean THEN $6::jsonb ELSE objective END
			WHERE id = $1 AND version < $2`,
			ev.EncounterID, e.StreamVersion, e.OccurredAt, ev.Subjective != nil, ev.Subjective, objective)
		return err

	case domain.DiagnosesSet:
		diags, err := jsonArg(ev.Diagnoses)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, diagnoses = $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, diags)
		return err

	case domain.PlanSet:
		plan, err := jsonArg(ev.Plan)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, plan = $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, plan)
		return err

	case domain.FollowUpRecommended:
		fu, err := jsonArg(map[string]any{"due_date": ev.DueDate, "reason": ev.Reason})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, follow_up = $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, fu)
		return err

	case domain.FollowUpCleared:
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, follow_up = NULL
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.AttendingVetAssigned:
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3, attending_vet_id = $4
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, ev.UserID)
		return err

	case domain.AttachmentAdded:
		_, err := tx.Exec(ctx, `
			INSERT INTO read_clinical.encounter_attachments
			  (id, encounter_id, object_key, content_type, size_bytes, description,
			   visible_to_guardian, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO NOTHING`,
			ev.Attachment.ID, ev.EncounterID, ev.Attachment.ObjectKey, string(ev.Attachment.ContentType),
			ev.Attachment.SizeBytes, ev.Attachment.Description, ev.Attachment.VisibleToGuardian, e.OccurredAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.AttachmentRemoved:
		if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.encounter_attachments
			WHERE id = $1 AND encounter_id = $2`, ev.AttachmentID, ev.EncounterID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.AttachmentVisibilityChanged:
		if _, err := tx.Exec(ctx, `UPDATE read_clinical.encounter_attachments
			SET visible_to_guardian = $3 WHERE id = $1 AND encounter_id = $2`,
			ev.AttachmentID, ev.EncounterID, ev.Visible); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters
			SET version = $2, updated_at = $3, last_activity_at = $3
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.EncounterSigned:
		sig, err := jsonArg(map[string]any{
			"signed_by": ev.SignedBy, "signed_at": ev.SignedAt,
			"stream_version": ev.StreamVersion, "content_hash": ev.ContentHash,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE read_clinical.encounters SET
			version = $2, updated_at = $3, last_activity_at = $3,
			status = 'signed', signature = $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, sig); err != nil {
			return err
		}
		return enqueuePDF(ctx, tx, ev.EncounterID, 1)

	case domain.EncounterAddendumAdded:
		add, err := jsonArg(ev)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE read_clinical.encounters SET
			version = $2, updated_at = $3, last_activity_at = $3,
			addenda = addenda || $4::jsonb
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt, add); err != nil {
			return err
		}
		for _, a := range ev.Attachments {
			if _, err := tx.Exec(ctx, `
				INSERT INTO read_clinical.encounter_attachments
				  (id, encounter_id, object_key, content_type, size_bytes, description,
				   visible_to_guardian, addendum_seq, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				ON CONFLICT (id) DO NOTHING`,
				a.ID, ev.EncounterID, a.ObjectKey, string(a.ContentType), a.SizeBytes,
				a.Description, a.VisibleToGuardian, ev.Seq, ev.SignedAt); err != nil {
				return err
			}
		}
		// Nueva versión del PDF por adenda (ARQ-010).
		return enqueuePDF(ctx, tx, ev.EncounterID, ev.Seq+1)

	case domain.EncounterDiscarded:
		_, err := tx.Exec(ctx, `UPDATE read_clinical.encounters SET
			version = $2, updated_at = $3, last_activity_at = $3, status = 'discarded'
			WHERE id = $1 AND version < $2`, ev.EncounterID, e.StreamVersion, e.OccurredAt)
		return err
	}
	return nil
}

// enqueuePDF encola el trabajo del worker (misma tx del comando).
func enqueuePDF(ctx context.Context, tx pgx.Tx, encounterID uuid.UUID, version int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.document_jobs (id, kind, ref_id, version, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', now(), now())
		ON CONFLICT (kind, ref_id, version) DO NOTHING`,
		uuid.New(), DocumentJobKindEncounterPDF, encounterID, version)
	return err
}

// emptyPlan devuelve el plan vacío inicial (JSON estable con listas vacías).
func emptyPlan() domain.Plan {
	return domain.Plan{Medications: []domain.MedicationItem{}, Procedures: []domain.Procedure{}}
}

// SyncProjector adapta el Runner a app.ProjectionRunner.
type SyncProjector struct{ runner *projection.Runner }

var _ app.ProjectionRunner = (*SyncProjector)(nil)

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

// RegisterSyncProjectors registra las proyecciones síncronas de Clinical.
func RegisterSyncProjectors(runner *projection.Runner, reg *eventstore.Registry) {
	runner.Register(NewEncountersProjector(reg))
	runner.Register(NewCardEntriesProjector(reg))
}
