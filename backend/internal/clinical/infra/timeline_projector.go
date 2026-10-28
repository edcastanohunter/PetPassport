package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// TimelineProjectorName es la clave estable del checkpoint (renombrar = rebuild).
const TimelineProjectorName = "clinical.medical_timeline"

// Tipos de ítem de read_clinical.medical_timeline (ARQ-012).
const (
	TimelineKindGuardianship = "guardianship"
	TimelineKindCareLink     = "care_link"
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
	return []string{"CareLinkGranted", "CareLinkRevoked", "PetGuardianshipTransferred"}
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
		item = timelineItem{petID: p.PetID, kind: TimelineKindCareLink, orgID: &org, title: title}
	case "PetGuardianshipTransferred":
		var p transferPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("timeline: %s: %w", e.Type, err)
		}
		item = timelineItem{petID: p.PetID, kind: TimelineKindGuardianship, title: "guardianship.transferred"}
	default:
		return nil
	}
	if item.petID == uuid.Nil {
		return fmt.Errorf("timeline: %s@%d sin pet_id", e.Type, e.GlobalPosition)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO read_clinical.medical_timeline (pet_id, position, occurred_at, kind, ref_id, organization_id, title, summary, visibility)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, 'guardian')
		ON CONFLICT (pet_id, position) DO NOTHING`,
		item.petID, e.GlobalPosition, e.OccurredAt, item.kind, e.EventID, item.orgID, item.title)
	if err != nil {
		return fmt.Errorf("timeline: insertar: %w", err)
	}
	return nil
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
	petID uuid.UUID
	kind  string
	orgID *uuid.UUID
	title string
}
