package infra

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/queue"
)

// PetsSearchProjector proyecta eventos del agregado Pet en la cola de indexación para OpenSearch (ARQ-001).
type PetsSearchProjector struct {
	queueClient *queue.Client
}

// NewPetsSearchProjector crea una nueva instancia del proyector.
func NewPetsSearchProjector(queueClient *queue.Client) *PetsSearchProjector {
	return &PetsSearchProjector{
		queueClient: queueClient,
	}
}

func (p *PetsSearchProjector) Name() string {
	return "search.pets"
}

func (p *PetsSearchProjector) Handles() []string {
	return []string{
		"PetRegistered",
		"PetProfileUpdated",
		"MicrochipRegistered",
		"PhotoRegistered",
		"PetDeceased",
		"PetGuardianshipTransferred",
		"PetClaimed",
		"CareLinkGranted",
		"CareLinkRevoked",
	}
}

func (p *PetsSearchProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "DELETE FROM search.index_queue WHERE index = 'pets'")
	return err
}

func (p *PetsSearchProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	petID := e.StreamID

	// Upsert en search.index_queue
	_, err := tx.Exec(ctx, `
		INSERT INTO search.index_queue (index, doc_id, pet_id, version, status, updated_at)
		VALUES ('pets', $1, $2, $3, 'pending', $4)
		ON CONFLICT (index, doc_id) DO UPDATE SET
			version = GREATEST(search.index_queue.version, EXCLUDED.version),
			status = 'pending',
			updated_at = EXCLUDED.updated_at
	`, petID.String(), petID, e.GlobalPosition, time.Now())
	if err != nil {
		return err
	}

	// Si hay cliente asynq, encolar tarea de bulk
	if p.queueClient != nil {
		_, _ = p.queueClient.EnqueueSearchBulk(ctx, "pets", []string{petID.String()})
		if e.Type == "CareLinkGranted" || e.Type == "CareLinkRevoked" || e.Type == "PetGuardianshipTransferred" {
			_, _ = p.queueClient.EnqueueSearchAccess(ctx, petID)
		}
	}

	return nil
}

// ClinicalSearchProjector proyecta eventos clínicos en la cola de indexación para OpenSearch (ARQ-001).
type ClinicalSearchProjector struct {
	queueClient *queue.Client
}

// NewClinicalSearchProjector crea una nueva instancia del proyector clínico.
func NewClinicalSearchProjector(queueClient *queue.Client) *ClinicalSearchProjector {
	return &ClinicalSearchProjector{
		queueClient: queueClient,
	}
}

func (c *ClinicalSearchProjector) Name() string {
	return "search.clinical"
}

func (c *ClinicalSearchProjector) Handles() []string {
	return []string{
		"EncounterCreated",
		"EncounterDraftUpdated",
		"EncounterSigned",
		"EncounterVoided",
		"EncounterAddended",
		"VaccineEntryRecorded",
		"DewormingEntryRecorded",
		"EctoparasiteEntryRecorded",
	}
}

func (c *ClinicalSearchProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "DELETE FROM search.index_queue WHERE index = 'clinical'")
	return err
}

func (c *ClinicalSearchProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	docID := e.StreamID.String()

	var petID *uuid.UUID
	// Extraer pet_id del payload JSON del evento
	var payload struct {
		PetID uuid.UUID `json:"pet_id"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err == nil && payload.PetID != uuid.Nil {
		petID = &payload.PetID
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO search.index_queue (index, doc_id, pet_id, version, status, updated_at)
		VALUES ('clinical', $1, $2, $3, 'pending', $4)
		ON CONFLICT (index, doc_id) DO UPDATE SET
			version = GREATEST(search.index_queue.version, EXCLUDED.version),
			status = 'pending',
			updated_at = EXCLUDED.updated_at
	`, docID, petID, e.GlobalPosition, time.Now())
	if err != nil {
		return err
	}

	// Si hay cliente asynq, encolar tarea de bulk
	if c.queueClient != nil {
		_, _ = c.queueClient.EnqueueSearchBulk(ctx, "clinical", []string{docID})
	}

	return nil
}
