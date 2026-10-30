package infra

import (
	"context"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
)

// Fuentes de la bandeja del tutor que viven en otros contextos (FE-012). Se
// leen por SQL sobre sus read models, como los demás adaptadores entre
// contextos (ADR-001: sin importar sus paquetes).

// ProposalInbox aporta las propuestas de cita pendientes de respuesta.
type ProposalInbox struct{ q rawQuerier }

var _ app.InboxSource = (*ProposalInbox)(nil)

// NewProposalInbox construye la fuente.
func NewProposalInbox(q rawQuerier) *ProposalInbox { return &ProposalInbox{q: q} }

// InboxItems implementa app.InboxSource.
func (s *ProposalInbox) InboxItems(ctx context.Context, guardianID uuid.UUID) ([]app.InboxItem, error) {
	rows, err := s.q.Query(ctx, `
		SELECT a.id, a.pet_id, coalesce(p.name, ''), a.organization_id, coalesce(o.display_name, ''),
		       a.reason, a.updated_at, a.proposal_expires_at
		  FROM read_scheduling.appointments a
		  LEFT JOIN read_petregistry.pets p ON p.id = a.pet_id
		  LEFT JOIN read_partnernetwork.organizations o ON o.id = a.organization_id
		 WHERE a.guardian_id = $1 AND a.status = 'proposed'
		   AND (a.proposal_expires_at IS NULL OR a.proposal_expires_at > now())
		 ORDER BY a.updated_at DESC, a.id DESC LIMIT 50`, guardianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.InboxItem{}
	for rows.Next() {
		it := app.InboxItem{Kind: "appointment_proposal", Status: "pending"}
		var petID, orgID uuid.UUID
		if err := rows.Scan(&it.RefID, &petID, &it.PetName, &orgID, &it.OrgName, &it.Body, &it.CreatedAt, &it.ExpiresAt); err != nil {
			return nil, err
		}
		it.ID, it.PetID, it.OrganizationID, it.Title = it.RefID, &petID, &orgID, it.OrgName
		out = append(out, it)
	}
	return out, rows.Err()
}

// FollowUpInbox aporta los controles pendientes (consultas firmadas con
// fecha de control de hoy en adelante).
type FollowUpInbox struct{ q rawQuerier }

var _ app.InboxSource = (*FollowUpInbox)(nil)

// NewFollowUpInbox construye la fuente.
func NewFollowUpInbox(q rawQuerier) *FollowUpInbox { return &FollowUpInbox{q: q} }

// InboxItems implementa app.InboxSource.
func (s *FollowUpInbox) InboxItems(ctx context.Context, guardianID uuid.UUID) ([]app.InboxItem, error) {
	rows, err := s.q.Query(ctx, `
		SELECT e.id, e.pet_id, p.name, e.organization_id, coalesce(o.display_name, ''),
		       coalesce(e.follow_up->>'reason', ''), e.updated_at, (e.follow_up->>'due_date')::date::timestamptz
		  FROM read_clinical.encounters e
		  JOIN read_petregistry.pets p ON p.id = e.pet_id AND p.guardian_id = $1
		  LEFT JOIN read_partnernetwork.organizations o ON o.id = e.organization_id
		 WHERE e.status = 'signed' AND e.follow_up IS NOT NULL
		   AND (e.follow_up->>'due_date')::date >= current_date
		 ORDER BY (e.follow_up->>'due_date') ASC, e.id DESC LIMIT 50`, guardianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.InboxItem{}
	for rows.Next() {
		it := app.InboxItem{Kind: "follow_up", Status: "pending"}
		var petID, orgID uuid.UUID
		if err := rows.Scan(&it.RefID, &petID, &it.PetName, &orgID, &it.OrgName, &it.Body, &it.CreatedAt, &it.ExpiresAt); err != nil {
			return nil, err
		}
		it.ID, it.PetID, it.OrganizationID, it.Title = it.RefID, &petID, &orgID, it.OrgName
		out = append(out, it)
	}
	return out, rows.Err()
}
