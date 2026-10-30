package infra

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CatalogAdminStore implementa app.CatalogAdmin: escribe ref.* y audita cada
// cambio en platform.audit_log en la misma transacción (API-005).
type CatalogAdminStore struct{ pool *pgxpool.Pool }

var _ app.CatalogAdmin = (*CatalogAdminStore)(nil)

// NewCatalogAdmin construye el almacén de administración de catálogos.
func NewCatalogAdmin(pool *pgxpool.Pool) *CatalogAdminStore { return &CatalogAdminStore{pool: pool} }

// ErrCatalogDuplicate es CATALOG_DUPLICATE: ya existe ese registro.
var ErrCatalogDuplicate = kernel.NewDomainError("CATALOG_DUPLICATE", "Ya existe un registro igual en el catálogo.")

func (s *CatalogAdminStore) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return ErrCatalogDuplicate
		}
		return err
	}
	return tx.Commit(ctx)
}

func auditCatalog(ctx context.Context, tx pgx.Tx, actor uuid.UUID, action, typ string, id uuid.UUID, reason string, details map[string]any) error {
	return audit.Record(ctx, tx, audit.Entry{
		ActorID: &actor, ActorRoles: []string{"super_admin"}, Action: action, TargetType: typ, TargetID: &id,
		Reason: reason, Details: details,
	})
}

// SaveVaccine crea (id nil) o actualiza una vacuna.
func (s *CatalogAdminStore) SaveVaccine(ctx context.Context, actor uuid.UUID, id *uuid.UUID, v app.VaccineRef, reason string) (app.VaccineRef, error) {
	create := id == nil
	if create {
		n := uuid.Must(uuid.NewV7())
		id = &n
	}
	v.ID, v.Active = *id, true
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var tag pgconn.CommandTag
		var err error
		if create {
			tag, err = tx.Exec(ctx, `INSERT INTO ref.vaccines (id, name, species, diseases, type, routes, active)
				VALUES ($1,$2,$3,$4,$5,$6,true)`, v.ID, v.Name, v.Species, v.Diseases, v.Type, v.Routes)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE ref.vaccines SET name=$2, species=$3, diseases=$4, type=$5, routes=$6,
				active=true, updated_at=now() WHERE id=$1`, v.ID, v.Name, v.Species, v.Diseases, v.Type, v.Routes)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &kernel.NotFoundError{Resource: "vaccine"}
		}
		action := "catalog.vaccine.update"
		if create {
			action = "catalog.vaccine.create"
		}
		return auditCatalog(ctx, tx, actor, action, "vaccine", v.ID, reason, map[string]any{"name": v.Name})
	})
	return v, err
}

// DeactivateVaccine da de baja lógica una vacuna.
func (s *CatalogAdminStore) DeactivateVaccine(ctx context.Context, actor, id uuid.UUID, reason string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ref.vaccines SET active=false, updated_at=now() WHERE id=$1 AND active`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &kernel.NotFoundError{Resource: "vaccine"}
		}
		return auditCatalog(ctx, tx, actor, "catalog.vaccine.deactivate", "vaccine", id, reason, nil)
	})
}

// SaveProtocol crea o actualiza un protocolo.
func (s *CatalogAdminStore) SaveProtocol(ctx context.Context, actor uuid.UUID, id *uuid.UUID, p app.ProtocolRef, reason string) (app.ProtocolRef, error) {
	create := id == nil
	if create {
		n := uuid.Must(uuid.NewV7())
		id = &n
	}
	p.ID, p.Active = *id, true
	series, err := json.Marshal(p.InitialSeries)
	if err != nil {
		return p, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var tag pgconn.CommandTag
		var err error
		if create {
			tag, err = tx.Exec(ctx, `INSERT INTO ref.vaccination_protocols
				(id, country, species, vaccine_id, initial_series, booster_interval_months, legal_validity_months, notes, active)
				VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,true)`,
				p.ID, p.Country, p.Species, p.VaccineID, series, p.BoosterIntervalMonths, p.LegalValidityMonths, p.Notes)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE ref.vaccination_protocols SET country=$2, species=$3, vaccine_id=$4,
				initial_series=$5::jsonb, booster_interval_months=$6, legal_validity_months=$7, notes=$8,
				active=true, updated_at=now() WHERE id=$1`,
				p.ID, p.Country, p.Species, p.VaccineID, series, p.BoosterIntervalMonths, p.LegalValidityMonths, p.Notes)
		}
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23503" {
				return &kernel.NotFoundError{Resource: "vaccine"}
			}
			return err
		}
		if tag.RowsAffected() == 0 {
			return &kernel.NotFoundError{Resource: "protocol"}
		}
		action := "catalog.protocol.update"
		if create {
			action = "catalog.protocol.create"
		}
		return auditCatalog(ctx, tx, actor, action, "vaccination_protocol", p.ID, reason,
			map[string]any{"country": p.Country, "species": p.Species, "vaccine_id": p.VaccineID.String()})
	})
	return p, err
}

// DeactivateProtocol da de baja lógica un protocolo.
func (s *CatalogAdminStore) DeactivateProtocol(ctx context.Context, actor, id uuid.UUID, reason string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ref.vaccination_protocols SET active=false, updated_at=now() WHERE id=$1 AND active`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &kernel.NotFoundError{Resource: "protocol"}
		}
		return auditCatalog(ctx, tx, actor, "catalog.protocol.deactivate", "vaccination_protocol", id, reason, nil)
	})
}
