package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// TimelineProjectorName es la clave estable del checkpoint (renombrar = rebuild).
const TimelineProjectorName = app.ProjectionTimeline

// Tipos de ítem de read_clinical.medical_timeline (ARQ-012).
const (
	TimelineKindGuardianship = "guardianship"
	TimelineKindCareLink     = "care_link"
	TimelineKindEncounter    = "encounter"
	TimelineKindAddendum     = "addendum"
)

// TimelineProjector proyecta read_clinical.medical_timeline (PP-F2-ARQ-002,
// proyección asíncrona; ARQ-012 define el read model completo). En M2.0
// consume los eventos de Pet Registry que ya existen (vínculos y
// transferencias) para demostrar el pipeline extremo a extremo; Encounter*,
// Vaccination*, Preventive* y AppointmentCompleted se añaden en M2.1–M2.3.
//
// Los payloads se leen con DTOs locales (el JSON persistido es el lenguaje
// publicado): Clinical no importa el dominio de Pet Registry (ADR-001).
// Idempotente por PK (pet_id, position): reproyectar no duplica.
//
// Título: clave estable que traduce el frontend; summary vacío (el motivo de
// revocación es texto libre del tutor y podría contener PII). Visibilidad
// `guardian`: los vínculos con una organización no se muestran a otras.
type TimelineProjector struct{}

var _ projection.AsyncProjector = TimelineProjector{}

// NewTimelineProjector construye el proyector.
func NewTimelineProjector() TimelineProjector { return TimelineProjector{} }

// Name implementa projection.Projector.
func (TimelineProjector) Name() string { return TimelineProjectorName }

// Handles implementa projection.Projector.
func (TimelineProjector) Handles() []string {
	return []string{
		"CareLinkGranted", "CareLinkRevoked", "PetGuardianshipTransferred",
		"EncounterSigned", "EncounterAddendumAdded",
	}
}

type careLinkPayload struct {
	PetID          uuid.UUID `json:"pet_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

type transferPayload struct {
	PetID uuid.UUID `json:"pet_id"`
}

// Project implementa projection.Projector.
func (TimelineProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var item timelineItem
	switch e.Type {
	case "CareLinkGranted", "CareLinkRevoked":
		var p careLinkPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("timeline: %s: %w", e.Type, err)
		}
		org := p.OrganizationID
		title := "care_link.granted"
		if e.Type == "CareLinkRevoked" {
			title = "care_link.revoked"
		}
		item = timelineItem{petID: p.PetID, kind: TimelineKindCareLink, orgID: &org, title: title, visibility: "guardian"}
	case "PetGuardianshipTransferred":
		var p transferPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("timeline: %s: %w", e.Type, err)
		}
		item = timelineItem{petID: p.PetID, kind: TimelineKindGuardianship, title: "guardianship.transferred", visibility: "guardian"}
	case "EncounterSigned":
		var p struct {
			EncounterID    uuid.UUID `json:"encounter_id"`
			PetID          uuid.UUID `json:"pet_id"`
			OrganizationID uuid.UUID `json:"organization_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("timeline: %s: %w", e.Type, err)
		}
		org := p.OrganizationID
		item = timelineItem{
			petID: p.PetID, kind: TimelineKindEncounter, refID: p.EncounterID,
			orgID: &org, title: "encounter.signed", visibility: "both",
		}
	case "EncounterAddendumAdded":
		var p struct {
			EncounterID uuid.UUID `json:"encounter_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("timeline: %s: %w", e.Type, err)
		}
		petID, org, err := encounterPetOrg(ctx, tx, p.EncounterID)
		if err != nil {
			return err
		}
		item = timelineItem{
			petID: petID, kind: TimelineKindAddendum, refID: p.EncounterID,
			orgID: &org, title: "encounter.addendum", visibility: "both",
		}
	default:
		return nil
	}
	if item.petID == uuid.Nil {
		return fmt.Errorf("timeline: %s@%d sin pet_id", e.Type, e.GlobalPosition)
	}
	if item.refID == uuid.Nil {
		item.refID = e.EventID
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.medical_timeline (pet_id, position, occurred_at, kind, ref_id, organization_id, title, summary, visibility)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, $8)
		ON CONFLICT (pet_id, position) DO NOTHING`,
		item.petID, e.GlobalPosition, e.OccurredAt, item.kind, item.refID, item.orgID, item.title, item.visibility)
	if err != nil {
		return fmt.Errorf("timeline: insertar: %w", err)
	}
	return nil
}

// encounterPetOrg resuelve pet y organización del encuentro ya proyectado
// (la proyección síncrona se aplica antes en el flujo del comando; en un
// replay el evento EncounterOpened precede a estos eventos).
func encounterPetOrg(ctx context.Context, tx pgx.Tx, encounterID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	var petID, orgID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT pet_id, organization_id FROM read_clinical.encounters WHERE id = $1`, encounterID).
		Scan(&petID, &orgID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("timeline: encuentro %s: %w", encounterID, err)
	}
	return petID, orgID, nil
}

// Reset implementa projection.AsyncProjector (rebuild, ARQ-006). DELETE y no
// TRUNCATE: pp_app_writer no tiene TRUNCATE (mínimo privilegio).
func (TimelineProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.medical_timeline`); err != nil {
		return fmt.Errorf("timeline: reset: %w", err)
	}
	return nil
}

type timelineItem struct {
	petID      uuid.UUID
	kind       string
	refID      uuid.UUID
	orgID      *uuid.UUID
	title      string
	visibility string
}
