package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// OrgsProjector proyecta read_partnernetwork.organizations, org_members,
// invitations y public_code_reservations (DAT-006). Idempotente por upserts
// y actualizaciones absolutas: proyectar dos veces = mismo estado (ARQ-005).
type OrgsProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*OrgsProjector)(nil)

// NewOrgsProjector construye el proyector.
func NewOrgsProjector(reg *eventstore.Registry) *OrgsProjector {
	return &OrgsProjector{reg: reg}
}

// Name implementa Projector.
func (p *OrgsProjector) Name() string { return "partnernetwork.organizations" }

// Handles implementa Projector.
func (p *OrgsProjector) Handles() []string {
	return []string{
		"OrganizationRegistered", "OrganizationProfileUpdated", "OrganizationVerificationReset",
		"OrganizationVerified", "OrganizationRejected", "OrganizationSuspended", "OrganizationReactivated",
		"MemberInvited", "InvitationRevoked", "MemberJoined", "MemberRoleChanged", "MemberRemoved",
	}
}

// Project aplica un evento sobre el read model en la tx del comando.
func (p *OrgsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("partnernetwork projector decode: %w", err)
	}
	switch ev := decoded.(type) {
	case domain.OrganizationRegistered:
		_, err := tx.Exec(ctx, `
			INSERT INTO read_partnernetwork.organizations
			  (id, version, type, legal_name, display_name, tax_country, tax_value, address, city, country,
			   contact_email, contact_phone, website, public_code, status, founder_id, member_count, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14,'pending_verification',$15,1,$16,$16)
			ON CONFLICT (id) DO UPDATE SET
			  version = EXCLUDED.version, type = EXCLUDED.type, legal_name = EXCLUDED.legal_name,
			  display_name = EXCLUDED.display_name, tax_country = EXCLUDED.tax_country, tax_value = EXCLUDED.tax_value,
			  address = EXCLUDED.address, city = EXCLUDED.city, country = EXCLUDED.country,
			  contact_email = EXCLUDED.contact_email, contact_phone = EXCLUDED.contact_phone,
			  website = EXCLUDED.website, public_code = EXCLUDED.public_code,
			  founder_id = EXCLUDED.founder_id, updated_at = EXCLUDED.updated_at`,
			ev.OrganizationID, e.StreamVersion, string(ev.Type), ev.LegalName, ev.DisplayName,
			ev.TaxID.Country, ev.TaxID.Value, addressJSON(ev.Address), ev.Address.City, ev.Address.Country,
			ev.Contact.Email, ev.Contact.Phone, ev.Contact.Website, ev.PublicCode, ev.FounderID, e.OccurredAt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO read_partnernetwork.public_code_reservations (public_code, organization_id)
			VALUES ($1, $2) ON CONFLICT (public_code) DO NOTHING`, ev.PublicCode, ev.OrganizationID); err != nil {
			return err
		}
		return upsertMember(ctx, tx, ev.OrganizationID, ev.FounderID, adminRoleFor(ev.Type), e)

	case domain.OrganizationProfileUpdated:
		c := ev.Changes
		_, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.organizations SET
			  version = $2, updated_at = $3,
			  legal_name = COALESCE($4, legal_name),
			  display_name = COALESCE($5, display_name),
			  tax_country = COALESCE($6, tax_country),
			  tax_value = COALESCE($7, tax_value),
			  address = COALESCE($8, address),
			  city = COALESCE($9, city),
			  contact_email = COALESCE($10, contact_email),
			  contact_phone = COALESCE($11, contact_phone),
			  website = COALESCE($12, website)
			WHERE id = $1`,
			ev.OrganizationID, e.StreamVersion, e.OccurredAt,
			optStr(c.LegalName), optStr(c.DisplayName),
			taxField(c.TaxID, func(t domain.TaxIDPayload) string { return t.Country }),
			taxField(c.TaxID, func(t domain.TaxIDPayload) string { return t.Value }),
			addressField(c.Address),
			addrCity(c.Address),
			contactField(c.Contact, func(x domain.ContactPayload) string { return x.Email }),
			contactField(c.Contact, func(x domain.ContactPayload) string { return x.Phone }),
			c.Website)
		return err

	case domain.OrganizationVerificationReset:
		_, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.organizations
			   SET version = $2, updated_at = $3, status = 'pending_verification', status_reason = $4
			 WHERE id = $1`, ev.OrganizationID, e.StreamVersion, e.OccurredAt, ev.Cause)
		return err

	case domain.OrganizationVerified:
		return setStatus(ctx, tx, ev.OrganizationID, e, "active", ev.Reason)

	case domain.OrganizationRejected:
		return setStatus(ctx, tx, ev.OrganizationID, e, "rejected", ev.Reason)

	case domain.OrganizationSuspended:
		return setStatus(ctx, tx, ev.OrganizationID, e, "suspended", ev.Reason)

	case domain.OrganizationReactivated:
		return setStatus(ctx, tx, ev.OrganizationID, e, "active", ev.Reason)

	case domain.MemberInvited:
		if ev.PII == nil {
			return fmt.Errorf("partnernetwork projector: MemberInvited sin PII")
		}
		hash := app.InvitationTokenHashFrom(ctx, ev.InvitationID)
		if hash == nil {
			// Replay/rebuild: el token no está en el evento; se invalida con un
			// hash aleatorio y la invitación debe reenviarse.
			hash = kernel.HashToken(kernel.RealTokenGenerator{}.NewToken())
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO read_partnernetwork.invitations
			  (id, organization_id, email, role, token_hash, status, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7)
			ON CONFLICT (id) DO NOTHING`,
			ev.InvitationID, ev.OrganizationID, string(ev.PII.Email), string(ev.Role),
			hash, ev.ExpiresAt, e.OccurredAt); err != nil {
			return err
		}
		return bumpVersion(ctx, tx, ev.OrganizationID, e)

	case domain.InvitationRevoked:
		if _, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.invitations SET status = 'revoked'
			 WHERE id = $1 AND status = 'pending'`, ev.InvitationID); err != nil {
			return err
		}
		return bumpVersion(ctx, tx, ev.OrganizationID, e)

	case domain.MemberJoined:
		if err := upsertMember(ctx, tx, ev.OrganizationID, ev.UserID, string(ev.Role), e); err != nil {
			return err
		}
		if ev.InvitationID != (uuid.UUID{}) {
			if _, err := tx.Exec(ctx, `
				UPDATE read_partnernetwork.invitations SET status = 'accepted'
				 WHERE id = $1 AND status = 'pending'`, ev.InvitationID); err != nil {
				return err
			}
		}
		return recountMembers(ctx, tx, ev.OrganizationID, e)

	case domain.MemberRoleChanged:
		if _, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.org_members SET role = $3
			 WHERE organization_id = $1 AND user_id = $2 AND status = 'active'`,
			ev.OrganizationID, ev.UserID, string(ev.To)); err != nil {
			return err
		}
		return bumpVersion(ctx, tx, ev.OrganizationID, e)

	case domain.MemberRemoved:
		if _, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.org_members
			   SET status = 'removed', removed_at = $3
			 WHERE organization_id = $1 AND user_id = $2 AND status = 'active'`,
			ev.OrganizationID, ev.UserID, e.OccurredAt); err != nil {
			return err
		}
		return recountMembers(ctx, tx, ev.OrganizationID, e)
	}
	return fmt.Errorf("partnernetwork projector: evento no manejado %s", e.Type)
}

// bumpVersion mantiene organizations.version == versión del stream para los
// eventos que no tocan la fila (ETag/If-Match de PATCH /organizations).
func bumpVersion(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, e eventstore.StoredEvent) error {
	_, err := tx.Exec(ctx, `
		UPDATE read_partnernetwork.organizations SET version = $2, updated_at = $3 WHERE id = $1`,
		orgID, e.StreamVersion, e.OccurredAt)
	return err
}

func upsertMember(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role string, e eventstore.StoredEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO read_partnernetwork.org_members (organization_id, user_id, role, status, joined_at)
		VALUES ($1, $2, $3, 'active', $4)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
		  role = EXCLUDED.role, status = 'active', joined_at = EXCLUDED.joined_at, removed_at = NULL`,
		orgID, userID, string(role), e.OccurredAt)
	return err
}

func recountMembers(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, e eventstore.StoredEvent) error {
	_, err := tx.Exec(ctx, `
		UPDATE read_partnernetwork.organizations SET version = $2, updated_at = $3,
		  member_count = (SELECT count(*) FROM read_partnernetwork.org_members
		                   WHERE organization_id = $1 AND status = 'active')
		WHERE id = $1`, orgID, e.StreamVersion, e.OccurredAt)
	return err
}

func setStatus(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, e eventstore.StoredEvent, status, reason string) error {
	_, err := tx.Exec(ctx, `
		UPDATE read_partnernetwork.organizations
		   SET version = $2, updated_at = $3, status = $4, status_reason = NULLIF($5,'')
		 WHERE id = $1`, orgID, e.StreamVersion, e.OccurredAt, status, reason)
	return err
}

// SyncProjector adapta projection.Runner al puerto de app (tx del contexto).
type SyncProjector struct {
	runner *projection.Runner
}

// NewSyncProjector construye el adaptador.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply aplica los proyectores en la tx del contexto. La unicidad del tax_id
// se aplica por índice único en la proyección (DAT-006): la violación se
// traduce a 422 VALIDATION_FAILED {field: tax_id, code: ALREADY_REGISTERED}.
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return translateUnique(a.runner.Apply(ctx, tx, events))
}

func translateUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "organizations_tax_idx" {
		return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "tax_id", Code: "ALREADY_REGISTERED"}}}
	}
	return err
}

// helpers --------------------------------------------------------------------

func adminRoleFor(t domain.OrganizationType) string {
	if t == domain.OrgTypeClinic {
		return string(domain.RoleClinicAdmin)
	}
	return string(domain.RolePharmacyAdmin)
}

func optStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func addressJSON(p domain.AddressPayload) []byte {
	raw, _ := json.Marshal(p)
	return raw
}

func addressField(p *domain.AddressPayload) any {
	if p == nil {
		return nil
	}
	return addressJSON(*p)
}

func addrCity(p *domain.AddressPayload) any {
	if p == nil {
		return nil
	}
	return p.City
}

func taxField(t *domain.TaxIDPayload, get func(domain.TaxIDPayload) string) any {
	if t == nil {
		return nil
	}
	return get(*t)
}

func contactField(c *domain.ContactPayload, get func(domain.ContactPayload) string) any {
	if c == nil {
		return nil
	}
	return get(*c)
}
