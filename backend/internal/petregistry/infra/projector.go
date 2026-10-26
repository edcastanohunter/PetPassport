package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PetsProjector proyecta read_petregistry.pets, pet_care_links y
// microchip_reservations (DAT-005). Idempotente por guarda de versión y
// upserts, para replay/rebuild sin cambios en F2 (ARQ-005): proyectar dos
// veces produce el mismo estado (prueba en test/integration).
type PetsProjector struct {
	reg      *eventstore.Registry
	guardian app.GuardianLookup
	orgs     app.OrganizationLookup
}

var _ projection.Projector = (*PetsProjector)(nil)

// NewPetsProjector construye el proyector. guardian denormaliza el nombre del
// tutor (DOM-010); nil = nombre vacío (pruebas sin Identity).
func NewPetsProjector(reg *eventstore.Registry, guardian app.GuardianLookup) *PetsProjector {
	return &PetsProjector{reg: reg, guardian: guardian}
}

// WithOrganizations inyecta el puerto de Partner Network para denormalizar
// nombre y ciudad de la clínica en pet_care_links (DAT-005).
func (p *PetsProjector) WithOrganizations(orgs app.OrganizationLookup) *PetsProjector {
	p.orgs = orgs
	return p
}

// Name implementa Projector.
func (p *PetsProjector) Name() string { return "petregistry.pets" }

// Handles implementa Projector.
func (p *PetsProjector) Handles() []string {
	return []string{
		"PetRegistered", "PetProfileUpdated", "PetMicrochipAssigned", "PetPhotoChanged",
		"PetMarkedDeceased", "PetDeceasedUndone", "CareLinkGranted", "CareLinkRevoked",
		"GuardianshipTransferInitiated", "GuardianshipTransferCancelled", "PetGuardianshipTransferred",
	}
}

// Project aplica un evento sobre el read model en la tx del comando.
func (p *PetsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("petregistry projector decode: %w", err)
	}
	switch ev := decoded.(type) {
	case domain.PetRegistered:
		name := ""
		if p.guardian != nil {
			name = p.guardian.DisplayName(ctx, ev.GuardianID)
		}
		var birthDate, markings, breed any
		approx := false
		if ev.BirthDate != nil {
			birthDate = ev.BirthDate.Date
			approx = ev.BirthDate.Approximate
		}
		if ev.Breed != "" {
			breed = ev.Breed
		}
		if ev.Markings != "" {
			markings = ev.Markings
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO read_petregistry.pets
			  (id, version, guardian_id, guardian_display_name, name, species, species_other, breed,
			   sex, neutered, birth_date, birth_date_approximate, markings, status, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'active',$14,$14)
			ON CONFLICT (id) DO UPDATE SET
			  version = EXCLUDED.version, guardian_id = EXCLUDED.guardian_id,
			  guardian_display_name = EXCLUDED.guardian_display_name, name = EXCLUDED.name,
			  species = EXCLUDED.species, species_other = EXCLUDED.species_other, breed = EXCLUDED.breed,
			  sex = EXCLUDED.sex, neutered = EXCLUDED.neutered, birth_date = EXCLUDED.birth_date,
			  birth_date_approximate = EXCLUDED.birth_date_approximate, markings = EXCLUDED.markings,
			  status = 'active', updated_at = EXCLUDED.updated_at
			WHERE read_petregistry.pets.version < EXCLUDED.version`,
			ev.PetID, e.StreamVersion, ev.GuardianID, name, ev.Name, string(ev.Species), ev.SpeciesOther,
			breed, string(ev.Sex), string(ev.Neutered), birthDate, approx, markings, e.OccurredAt)
		return err

	case domain.PetProfileUpdated:
		c := ev.Changes
		// birth: hasBirth distingue "sin cambio" de "vaciar" (date nil).
		hasBirth := c.BirthDate != nil
		var birthDate any
		approx := false
		if hasBirth && c.BirthDate.Date != "" {
			birthDate = c.BirthDate.Date
			approx = c.BirthDate.Approximate
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET
			  version = $2, updated_at = $3,
			  name = COALESCE($4, name),
			  species = COALESCE($5, species),
			  species_other = CASE
			    WHEN $5 IS NOT NULL AND $5 <> 'other' THEN NULL
			    ELSE COALESCE($6, species_other) END,
			  breed = CASE WHEN $7 THEN $8 ELSE breed END,
			  sex = COALESCE($9, sex),
			  neutered = COALESCE($10, neutered),
			  birth_date = CASE WHEN $11 THEN $12 ELSE birth_date END,
			  birth_date_approximate = CASE WHEN $11 THEN $13 ELSE birth_date_approximate END,
			  markings = CASE WHEN $14 THEN $15 ELSE markings END
			WHERE id = $1`,
			ev.PetID, e.StreamVersion, e.OccurredAt,
			optStr(c.Name), optStr(c.Species), optStr(c.SpeciesOther),
			c.Breed != nil, optNullable(c.Breed),
			optStr(c.Sex), optStr(c.Neutered),
			hasBirth, birthDate, approx,
			c.Markings != nil, optNullable(c.Markings))
		return err

	case domain.PetMicrochipAssigned:
		if ev.Previous != "" {
			if _, err := tx.Exec(ctx, `DELETE FROM read_petregistry.microchip_reservations
				 WHERE microchip = $1 AND pet_id = $2`, ev.Previous, ev.PetID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO read_petregistry.microchip_reservations (microchip, pet_id) VALUES ($1, $2)
			ON CONFLICT (microchip) DO NOTHING`, ev.Microchip, ev.PetID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3, microchip = $4,
			  microchip_corrections = microchip_corrections + $5
			WHERE id = $1`,
			ev.PetID, e.StreamVersion, e.OccurredAt, ev.Microchip, boolInt(ev.Previous != ""))
		return err

	case domain.PetPhotoChanged:
		var objectKey, thumbKey, contentType any
		if ev.Photo != nil {
			objectKey, thumbKey, contentType = ev.Photo.ObjectKey, ev.Photo.ThumbKey, ev.Photo.ContentType
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3,
			  photo_object_key = $4, photo_thumb_key = $5, photo_content_type = $6
			WHERE id = $1`,
			ev.PetID, e.StreamVersion, e.OccurredAt, objectKey, thumbKey, contentType)
		return err

	case domain.PetMarkedDeceased:
		var deceasedAt any
		if ev.DeceasedAt != "" {
			deceasedAt = ev.DeceasedAt
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3,
			  status = 'deceased', deceased_at = $4, deceased_marked_at = $3
			WHERE id = $1`, ev.PetID, e.StreamVersion, e.OccurredAt, deceasedAt)
		return err

	case domain.PetDeceasedUndone:
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3,
			  status = 'active', deceased_at = NULL, deceased_marked_at = NULL
			WHERE id = $1`, ev.PetID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.CareLinkGranted:
		// Nombre y ciudad desnormalizados (DOM-010). En rebuild la org puede no
		// estar activa: se conserva lo ya proyectado (COALESCE con NULLIF).
		name, city := "", ""
		if p.orgs != nil {
			if org, err := p.orgs.ActiveClinic(ctx, ev.OrganizationID); err == nil {
				name, city = org.DisplayName, org.City
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO read_petregistry.pet_care_links
			  (pet_id, organization_id, org_display_name, org_city, status, granted_by, granted_at)
			VALUES ($1, $2, $5, NULLIF($6, ''), 'active', $3, $4)
			ON CONFLICT (pet_id, organization_id) DO UPDATE SET
			  status = 'active', granted_by = EXCLUDED.granted_by, granted_at = EXCLUDED.granted_at,
			  revoked_at = NULL,
			  org_display_name = COALESCE(NULLIF(EXCLUDED.org_display_name, ''), read_petregistry.pet_care_links.org_display_name),
			  org_city = COALESCE(EXCLUDED.org_city, read_petregistry.pet_care_links.org_city)`,
			ev.PetID, ev.OrganizationID, ev.GrantedBy, e.OccurredAt, name, city); err != nil {
			return err
		}
		return p.recountLinks(ctx, tx, ev.PetID, e)

	case domain.CareLinkRevoked:
		if _, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_care_links SET status = 'revoked', revoked_at = $3
			 WHERE pet_id = $1 AND organization_id = $2 AND status = 'active'`,
			ev.PetID, ev.OrganizationID, e.OccurredAt); err != nil {
			return err
		}
		return p.recountLinks(ctx, tx, ev.PetID, e)

	case domain.GuardianshipTransferInitiated:
		masked := ""
		if ev.PII != nil {
			masked = ev.PII.ToEmail.Masked()
		}
		payload, err := json.Marshal(app.PendingTransferView{ToEmailMasked: masked, ExpiresAt: ev.ExpiresAt})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3, pending_transfer = $4
			 WHERE id = $1`, ev.PetID, e.StreamVersion, e.OccurredAt, payload); err != nil {
			return err
		}
		// pet_transfers resuelve token → mascota (DAT-005). El hash llega por el
		// contexto del comando; en replay/rebuild no existe (el token no está en
		// el evento) y las filas operativas se dejan como están: proyectar dos
		// veces = mismo estado.
		hash := app.TransferTokenHashFrom(ctx, ev.PetID)
		if hash == nil {
			return nil
		}
		emailHash := kernel.HashToken("")
		if ev.PII != nil {
			emailHash = kernel.HashToken(ev.PII.ToEmail.String())
		}
		if _, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_transfers SET status = 'cancelled'
			 WHERE pet_id = $1 AND status = 'pending' AND token_hash <> $2`, ev.PetID, hash); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO read_petregistry.pet_transfers (token_hash, pet_id, to_email_hash, expires_at, status)
			VALUES ($1, $2, $3, $4, 'pending')
			ON CONFLICT (token_hash) DO NOTHING`, hash, ev.PetID, emailHash, ev.ExpiresAt)
		return err

	case domain.GuardianshipTransferCancelled:
		if _, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_transfers SET status = 'cancelled'
			 WHERE pet_id = $1 AND status = 'pending'`, ev.PetID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3, pending_transfer = NULL
			 WHERE id = $1`, ev.PetID, e.StreamVersion, e.OccurredAt)
		return err

	case domain.PetGuardianshipTransferred:
		name := ""
		if p.guardian != nil {
			name = p.guardian.DisplayName(ctx, ev.ToGuardianID)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_transfers SET status = 'accepted'
			 WHERE pet_id = $1 AND status = 'pending'`, ev.PetID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pets SET version = $2, updated_at = $3,
			  guardian_id = $4, guardian_display_name = $5, pending_transfer = NULL
			 WHERE id = $1`, ev.PetID, e.StreamVersion, e.OccurredAt, ev.ToGuardianID, name)
		return err
	}
	return fmt.Errorf("petregistry projector: evento no manejado %s", e.Type)
}

func (p *PetsProjector) recountLinks(ctx context.Context, tx pgx.Tx, petID uuid.UUID, e eventstore.StoredEvent) error {
	_, err := tx.Exec(ctx, `
		UPDATE read_petregistry.pets SET version = $2, updated_at = $3,
		  active_links_count = (SELECT count(*) FROM read_petregistry.pet_care_links
		                         WHERE pet_id = $1 AND status = 'active')
		WHERE id = $1`, petID, e.StreamVersion, e.OccurredAt)
	return err
}

// SyncProjector adapta projection.Runner al puerto de app (tx del contexto).
type SyncProjector struct {
	runner *projection.Runner
}

var _ app.ProjectionRunner = (*SyncProjector)(nil)

// NewSyncProjector construye el adaptador.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply aplica los proyectores en la tx del contexto (ARQ-005).
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return a.runner.Apply(ctx, tx, events)
}

// helpers -------------------------------------------------------------------

func optStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// optNullable: puntero nil → nil (COALESCE lo ignora); puntero válido → valor
// (cadena vacía vacía el campo en los CASE explícitos).
func optNullable(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
