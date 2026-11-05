// Package infra implementa la persistencia en PostgreSQL y auditoría del catálogo de medicamentos (PP-F3-DAT-002, ADR-016).
package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	ErrNotFound             = kernel.NewDomainError("MEDICATION_NOT_FOUND", "Medicamento no encontrado en el catálogo.")
	ErrDiscontinued         = kernel.NewDomainError("MEDICATION_DISCONTINUED", "El medicamento está descontinuado.")
	ErrDuplicateIdentity    = kernel.NewDomainError("MEDICATION_DUPLICATE", "Ya existe un medicamento activo con el mismo nombre, fabricante, forma y concentración.")
	ErrMergeChainTooDeep    = kernel.NewDomainError("MERGE_CHAIN_TOO_DEEP", "La cadena de fusión no puede superar 3 saltos.")
	ErrInvalidMergeTarget   = kernel.NewDomainError("INVALID_MERGE_TARGET", "El medicamento destino no puede ser el mismo ni estar descontinuado.")
	ErrSuggestionNotFound   = kernel.NewDomainError("SUGGESTION_NOT_FOUND", "Sugerencia no encontrada.")
	ErrSuggestionProcessed  = kernel.NewDomainError("SUGGESTION_ALREADY_PROCESSED", "La sugerencia ya ha sido revisada previamente.")
	ErrBatchNotFound        = kernel.NewDomainError("IMPORT_BATCH_NOT_FOUND", "Lote de importación no encontrado.")
	ErrBatchAlreadyReverted = kernel.NewDomainError("IMPORT_BATCH_ALREADY_REVERTED", "El lote ya fue revertido.")
)

// Store gestiona las operaciones SQL sobre el esquema ref para medicamentos.
type Store struct {
	pool *pgxpool.Pool
}

var _ app.MedicationRepository = (*Store)(nil)

// NewStore crea una nueva instancia del almacén.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateMedication inserta un medicamento en ref.medications con auditoría y encolado de cambio.
func (s *Store) CreateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.Must(uuid.NewV7())
	}
	m.NameNorm = domain.Normalize(m.Name)
	if m.Manufacturer != nil {
		normM := domain.Normalize(*m.Manufacturer)
		m.ManufacturerNorm = &normM
	}
	if m.Status == "" {
		m.Status = domain.StatusActive
	}
	m.CreatedAt = time.Now()
	m.UpdatedAt = m.CreatedAt

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	actJSON, _ := json.Marshal(m.ActiveIngredients)
	regJSON, _ := json.Marshal(m.Regulatory)
	srcJSON, _ := json.Marshal(m.Source)

	_, err = tx.Exec(ctx, `
		INSERT INTO ref.medications (
			id, name, name_norm, active_ingredients, form, concentration, presentation,
			routes, species, manufacturer, manufacturer_norm, atcvet_code, regulatory,
			controlled, prescription_required, default_dose_unit, default_route,
			synonyms, status, merged_into, source, usage_count, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23, $24
		)`,
		m.ID, m.Name, m.NameNorm, actJSON, string(m.Form), m.Concentration, m.Presentation,
		m.Routes, m.Species, m.Manufacturer, m.ManufacturerNorm, m.ATCvetCode, regJSON,
		m.Controlled, m.PrescriptionRequired, m.DefaultDoseUnit, m.DefaultRoute,
		m.Synonyms, string(m.Status), m.MergedInto, srcJSON, m.UsageCount, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrDuplicateIdentity
		}
		return nil, fmt.Errorf("insert medication: %w", err)
	}

	// Encolar cambio para reindexación OpenSearch
	_, err = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, m.ID)
	if err != nil {
		return nil, fmt.Errorf("enqueue medication change: %w", err)
	}

	// Auditoría
	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:medication:create",
		TargetType: "medication",
		TargetID:   &m.ID,
		Reason:     reason,
		Details:    map[string]any{"name": m.Name, "form": m.Form},
	}); err != nil {
		return nil, fmt.Errorf("audit create: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateMedication actualiza los campos permitidos de un medicamento activo.
func (s *Store) UpdateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error) {
	m.NameNorm = domain.Normalize(m.Name)
	if m.Manufacturer != nil {
		normM := domain.Normalize(*m.Manufacturer)
		m.ManufacturerNorm = &normM
	}
	m.UpdatedAt = time.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	actJSON, _ := json.Marshal(m.ActiveIngredients)
	regJSON, _ := json.Marshal(m.Regulatory)

	tag, err := tx.Exec(ctx, `
		UPDATE ref.medications SET
			name = $2, name_norm = $3, active_ingredients = $4, form = $5,
			concentration = $6, presentation = $7, routes = $8, species = $9,
			manufacturer = $10, manufacturer_norm = $11, atcvet_code = $12, regulatory = $13,
			controlled = $14, prescription_required = $15, default_dose_unit = $16,
			default_route = $17, synonyms = $18, updated_at = $19
		WHERE id = $1 AND status <> 'merged'
	`, m.ID, m.Name, m.NameNorm, actJSON, string(m.Form), m.Concentration, m.Presentation,
		m.Routes, m.Species, m.Manufacturer, m.ManufacturerNorm, m.ATCvetCode, regJSON,
		m.Controlled, m.PrescriptionRequired, m.DefaultDoseUnit, m.DefaultRoute,
		m.Synonyms, m.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrDuplicateIdentity
		}
		return nil, fmt.Errorf("update medication: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}

	_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, m.ID)

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:medication:update",
		TargetType: "medication",
		TargetID:   &m.ID,
		Reason:     reason,
		Details:    map[string]any{"name": m.Name},
	}); err != nil {
		return nil, fmt.Errorf("audit update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// DiscontinueMedication marca un medicamento como discontinued con motivo. No lo borra (ADR-016).
func (s *Store) DiscontinueMedication(ctx context.Context, actor uuid.UUID, id uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE ref.medications
		SET status = 'discontinued', updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, id)

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:medication:discontinue",
		TargetType: "medication",
		TargetID:   &id,
		Reason:     reason,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// MergeMedication fusiona sourceID en targetID (máximo 3 saltos). El origen pasa a merged.
func (s *Store) MergeMedication(ctx context.Context, actor uuid.UUID, sourceID, targetID uuid.UUID, reason string) error {
	if sourceID == targetID {
		return ErrInvalidMergeTarget
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Validar estado del destino y profundidad de cadena <= 3
	var (
		curTarget   = targetID
		hops        = 0
		finalTarget = targetID
	)

	for {
		hops++
		if hops > 3 {
			return ErrMergeChainTooDeep
		}
		var st string
		var mergedInto *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT status, merged_into FROM ref.medications WHERE id = $1`, curTarget).Scan(&st, &mergedInto)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if st == string(domain.StatusDiscontinued) {
			return ErrInvalidMergeTarget
		}
		if st == string(domain.StatusActive) {
			finalTarget = curTarget
			break
		}
		if st == string(domain.StatusMerged) && mergedInto != nil {
			if *mergedInto == sourceID {
				return ErrInvalidMergeTarget // Ciclo detectado
			}
			curTarget = *mergedInto
			continue
		}
		break
	}

	// Marcar origen como merged
	tag, err := tx.Exec(ctx, `
		UPDATE ref.medications
		SET status = 'merged', merged_into = $2, updated_at = now()
		WHERE id = $1 AND status <> 'merged'
	`, sourceID, finalTarget)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	// Sumar usage_count de origen a destino
	var srcUsage int
	_ = tx.QueryRow(ctx, `SELECT usage_count FROM ref.medications WHERE id = $1`, sourceID).Scan(&srcUsage)
	if srcUsage > 0 {
		_, _ = tx.Exec(ctx, `UPDATE ref.medications SET usage_count = usage_count + $2 WHERE id = $1`, finalTarget, srcUsage)
	}

	// Encolar ambos para reindexación (origen se borrará en search, destino se actualizará)
	_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1), ($2)`, sourceID, finalTarget)

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:medication:merge",
		TargetType: "medication",
		TargetID:   &sourceID,
		Reason:     reason,
		Details:    map[string]any{"merged_into": finalTarget},
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetMedication obtiene un medicamento por ID. Si está fusionado, resuelve la redirección y devuelve el destino con mergedFrom.
func (s *Store) GetMedication(ctx context.Context, id uuid.UUID) (*domain.Medication, *uuid.UUID, error) {
	cur := id
	var mergedFrom *uuid.UUID
	hops := 0

	for {
		hops++
		if hops > 4 {
			return nil, nil, ErrMergeChainTooDeep
		}
		m, err := s.getOne(ctx, cur)
		if err != nil {
			return nil, nil, err
		}
		if m.Status == domain.StatusMerged && m.MergedInto != nil {
			if mergedFrom == nil {
				mergedFrom = &id
			}
			cur = *m.MergedInto
			continue
		}
		return m, mergedFrom, nil
	}
}

func (s *Store) getOne(ctx context.Context, id uuid.UUID) (*domain.Medication, error) {
	var (
		m       domain.Medication
		actJSON []byte
		regJSON []byte
		srcJSON []byte
		form    string
		st      string
	)

	err := s.pool.QueryRow(ctx, `
		SELECT id, name, name_norm, active_ingredients, form, concentration, presentation,
		       routes, species, manufacturer, manufacturer_norm, atcvet_code, regulatory,
		       controlled, prescription_required, default_dose_unit, default_route,
		       synonyms, status, merged_into, source, usage_count, created_at, updated_at
		FROM ref.medications
		WHERE id = $1
	`, id).Scan(
		&m.ID, &m.Name, &m.NameNorm, &actJSON, &form, &m.Concentration, &m.Presentation,
		&m.Routes, &m.Species, &m.Manufacturer, &m.ManufacturerNorm, &m.ATCvetCode, &regJSON,
		&m.Controlled, &m.PrescriptionRequired, &m.DefaultDoseUnit, &m.DefaultRoute,
		&m.Synonyms, &st, &m.MergedInto, &srcJSON, &m.UsageCount, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	m.Form = domain.Form(form)
	m.Status = domain.Status(st)
	_ = json.Unmarshal(actJSON, &m.ActiveIngredients)
	if len(regJSON) > 0 {
		_ = json.Unmarshal(regJSON, &m.Regulatory)
	}
	_ = json.Unmarshal(srcJSON, &m.Source)
	return &m, nil
}

// ResolveActiveMedication resuelve el catalog_id siguiendo hasta 3 saltos de fusión (PP-F3-DOM-006).
func (s *Store) ResolveActiveMedication(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	m, _, err := s.GetMedication(ctx, id)
	if err != nil {
		return uuid.Nil, "", err
	}
	if m.Status != domain.StatusActive {
		return uuid.Nil, "", ErrDiscontinued
	}
	return m.ID, m.Name, nil
}

// ListMedications lista medicamentos según los filtros provistos.
func (s *Store) ListMedications(ctx context.Context, f app.MedicationFilter) ([]domain.Medication, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT id, name, name_norm, active_ingredients, form, concentration, presentation,
		       routes, species, manufacturer, manufacturer_norm, atcvet_code, regulatory,
		       controlled, prescription_required, default_dose_unit, default_route,
		       synonyms, status, merged_into, source, usage_count, created_at, updated_at
		FROM ref.medications
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR $2 = ANY(species))
		  AND ($3 = '' OR form = $3)
		  AND ($4::boolean IS NULL OR prescription_required = $4)
		  AND ($5 = '' OR name_norm % domain.Normalize($5) OR name ILIKE '%' || $5 || '%')
		ORDER BY name ASC
		LIMIT $6
	`

	rows, err := s.pool.Query(ctx, query, f.Status, f.Species, f.Form, f.PrescriptionRequired, f.Q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Medication
	for rows.Next() {
		var (
			m       domain.Medication
			actJSON []byte
			regJSON []byte
			srcJSON []byte
			form    string
			st      string
		)
		if err := rows.Scan(
			&m.ID, &m.Name, &m.NameNorm, &actJSON, &form, &m.Concentration, &m.Presentation,
			&m.Routes, &m.Species, &m.Manufacturer, &m.ManufacturerNorm, &m.ATCvetCode, &regJSON,
			&m.Controlled, &m.PrescriptionRequired, &m.DefaultDoseUnit, &m.DefaultRoute,
			&m.Synonyms, &st, &m.MergedInto, &srcJSON, &m.UsageCount, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		m.Form = domain.Form(form)
		m.Status = domain.Status(st)
		_ = json.Unmarshal(actJSON, &m.ActiveIngredients)
		if len(regJSON) > 0 {
			_ = json.Unmarshal(regJSON, &m.Regulatory)
		}
		_ = json.Unmarshal(srcJSON, &m.Source)
		list = append(list, m)
	}
	return list, rows.Err()
}

// CreateSuggestion registra una nueva sugerencia de medicamento propuesta por un veterinario.
func (s *Store) CreateSuggestion(ctx context.Context, sug domain.MedicationSuggestion) (*domain.MedicationSuggestion, error) {
	if sug.ID == uuid.Nil {
		sug.ID = uuid.Must(uuid.NewV7())
	}
	sug.Status = domain.SuggestionPending
	sug.CreatedAt = time.Now()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO ref.medication_suggestions (
			id, proposed_by, organization_id, name, notes, photo_object_key, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, sug.ID, sug.ProposedBy, sug.OrganizationID, sug.Name, sug.Notes, sug.PhotoObjectKey, string(sug.Status), sug.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sug, nil
}

// ListSuggestions lista sugerencias filtradas opcionalmente por organización y estado.
func (s *Store) ListSuggestions(ctx context.Context, orgID *uuid.UUID, status *string) ([]domain.MedicationSuggestion, error) {
	query := `
		SELECT id, proposed_by, organization_id, name, notes, photo_object_key, status,
		       reviewed_by, reason, medication_id, created_at, reviewed_at
		FROM ref.medication_suggestions
		WHERE ($1::uuid IS NULL OR organization_id = $1)
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
	`
	stVal := ""
	if status != nil {
		stVal = *status
	}

	rows, err := s.pool.Query(ctx, query, orgID, stVal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.MedicationSuggestion
	for rows.Next() {
		var (
			sug domain.MedicationSuggestion
			st  string
		)
		if err := rows.Scan(
			&sug.ID, &sug.ProposedBy, &sug.OrganizationID, &sug.Name, &sug.Notes, &sug.PhotoObjectKey,
			&st, &sug.ReviewedBy, &sug.Reason, &sug.MedicationID, &sug.CreatedAt, &sug.ReviewedAt,
		); err != nil {
			return nil, err
		}
		sug.Status = domain.SuggestionStatus(st)
		list = append(list, sug)
	}
	return list, rows.Err()
}

// ApproveSuggestion aprueba una sugerencia creando el medicamento con source = suggestion.
func (s *Store) ApproveSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, m domain.Medication) (*domain.Medication, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM ref.medication_suggestions WHERE id = $1 FOR UPDATE`, suggestionID).Scan(&currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSuggestionNotFound
		}
		return nil, err
	}
	if currentStatus != string(domain.SuggestionPending) {
		return nil, ErrSuggestionProcessed
	}

	if m.ID == uuid.Nil {
		m.ID = uuid.Must(uuid.NewV7())
	}
	m.NameNorm = domain.Normalize(m.Name)
	if m.Manufacturer != nil {
		normM := domain.Normalize(*m.Manufacturer)
		m.ManufacturerNorm = &normM
	}
	m.Status = domain.StatusActive
	now := time.Now()
	m.CreatedAt = now
	m.UpdatedAt = now
	m.Source = domain.Source{
		Kind:       "suggestion",
		Ref:        suggestionID.String(),
		ImportedAt: &now,
	}

	actJSON, _ := json.Marshal(m.ActiveIngredients)
	regJSON, _ := json.Marshal(m.Regulatory)
	srcJSON, _ := json.Marshal(m.Source)

	_, err = tx.Exec(ctx, `
		INSERT INTO ref.medications (
			id, name, name_norm, active_ingredients, form, concentration, presentation,
			routes, species, manufacturer, manufacturer_norm, atcvet_code, regulatory,
			controlled, prescription_required, default_dose_unit, default_route,
			synonyms, status, merged_into, source, usage_count, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23, $24
		)`,
		m.ID, m.Name, m.NameNorm, actJSON, string(m.Form), m.Concentration, m.Presentation,
		m.Routes, m.Species, m.Manufacturer, m.ManufacturerNorm, m.ATCvetCode, regJSON,
		m.Controlled, m.PrescriptionRequired, m.DefaultDoseUnit, m.DefaultRoute,
		m.Synonyms, string(m.Status), m.MergedInto, srcJSON, m.UsageCount, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrDuplicateIdentity
		}
		return nil, fmt.Errorf("insert approved medication: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE ref.medication_suggestions
		SET status = 'approved', reviewed_by = $2, reviewed_at = $3, medication_id = $4
		WHERE id = $1
	`, suggestionID, actor, now, m.ID)
	if err != nil {
		return nil, err
	}

	_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, m.ID)

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:suggestion:approve",
		TargetType: "medication_suggestion",
		TargetID:   &suggestionID,
		Details:    map[string]any{"medication_id": m.ID},
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// RejectSuggestion rechaza una sugerencia registrando el motivo.
func (s *Store) RejectSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now()
	tag, err := tx.Exec(ctx, `
		UPDATE ref.medication_suggestions
		SET status = 'rejected', reviewed_by = $2, reviewed_at = $3, reason = $4
		WHERE id = $1 AND status = 'pending'
	`, suggestionID, actor, now, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSuggestionProcessed
	}

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:suggestion:reject",
		TargetType: "medication_suggestion",
		TargetID:   &suggestionID,
		Reason:     reason,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// SaveImportBatch persiste un lote de importación y sus filas asociadas.
func (s *Store) SaveImportBatch(ctx context.Context, batch domain.ImportBatch, rows []domain.ImportRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	errJSON, _ := json.Marshal(batch.Errors)
	_, err = tx.Exec(ctx, `
		INSERT INTO ref.medication_import_batches (
			id, source, status, dry_run, created, updated, skipped, errors, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, batch.ID, batch.Source, batch.Status, batch.DryRun, batch.Created, batch.Updated, batch.Skipped, errJSON, batch.CreatedBy, batch.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert batch: %w", err)
	}

	for _, r := range rows {
		_, err = tx.Exec(ctx, `
			INSERT INTO ref.medication_import_rows (batch_id, medication_id, action, previous)
			VALUES ($1, $2, $3, $4)
		`, r.BatchID, r.MedicationID, r.Action, r.Previous)
		if err != nil {
			return fmt.Errorf("insert batch row: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetImportBatch consulta un lote por id.
func (s *Store) GetImportBatch(ctx context.Context, id uuid.UUID) (*domain.ImportBatch, error) {
	var (
		b       domain.ImportBatch
		errJSON []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, source, status, dry_run, created, updated, skipped, errors, created_by, created_at, reverted_at
		FROM ref.medication_import_batches
		WHERE id = $1
	`, id).Scan(
		&b.ID, &b.Source, &b.Status, &b.DryRun, &b.Created, &b.Updated, &b.Skipped,
		&errJSON, &b.CreatedBy, &b.CreatedAt, &b.RevertedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBatchNotFound
		}
		return nil, err
	}
	if len(errJSON) > 0 {
		_ = json.Unmarshal(errJSON, &b.Errors)
	}
	return &b, nil
}

// RevertImportBatch revierte lógicamente un lote de importación.
func (s *Store) RevertImportBatch(ctx context.Context, actor uuid.UUID, batchID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM ref.medication_import_batches WHERE id = $1 FOR UPDATE`, batchID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBatchNotFound
		}
		return err
	}
	if status == "reverted" {
		return ErrBatchAlreadyReverted
	}

	rows, err := tx.Query(ctx, `SELECT medication_id, action, previous FROM ref.medication_import_rows WHERE batch_id = $1`, batchID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rowAction struct {
		medID    uuid.UUID
		action   string
		previous []byte
	}
	var actions []rowAction
	for rows.Next() {
		var ra rowAction
		if err := rows.Scan(&ra.medID, &ra.action, &ra.previous); err != nil {
			return err
		}
		actions = append(actions, ra)
	}

	for _, a := range actions {
		if a.action == "created" {
			_, _ = tx.Exec(ctx, `UPDATE ref.medications SET status = 'discontinued', updated_at = now() WHERE id = $1`, a.medID)
			_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, a.medID)
		} else if a.action == "updated" && len(a.previous) > 0 {
			var prev domain.Medication
			if err := json.Unmarshal(a.previous, &prev); err == nil {
				actJSON, _ := json.Marshal(prev.ActiveIngredients)
				regJSON, _ := json.Marshal(prev.Regulatory)
				_, _ = tx.Exec(ctx, `
					UPDATE ref.medications SET
						name = $2, active_ingredients = $3, form = $4, concentration = $5,
						presentation = $6, routes = $7, species = $8, manufacturer = $9,
						atcvet_code = $10, regulatory = $11, controlled = $12,
						prescription_required = $13, default_dose_unit = $14,
						default_route = $15, synonyms = $16, updated_at = now()
					WHERE id = $1
				`, prev.ID, prev.Name, actJSON, string(prev.Form), prev.Concentration,
					prev.Presentation, prev.Routes, prev.Species, prev.Manufacturer,
					prev.ATCvetCode, regJSON, prev.Controlled, prev.PrescriptionRequired,
					prev.DefaultDoseUnit, prev.DefaultRoute, prev.Synonyms)
				_, _ = tx.Exec(ctx, `INSERT INTO ref.medications_changes (medication_id) VALUES ($1)`, a.medID)
			}
		}
	}

	now := time.Now()
	_, err = tx.Exec(ctx, `UPDATE ref.medication_import_batches SET status = 'reverted', reverted_at = $2 WHERE id = $1`, batchID, now)
	if err != nil {
		return err
	}

	if err := audit.Record(ctx, tx, audit.Entry{
		ActorID:    &actor,
		ActorRoles: []string{"super_admin"},
		Action:     "catalog:batch:revert",
		TargetType: "medication_import_batch",
		TargetID:   &batchID,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UsageStats calcula el total de ítems en planes, cuántos usan catalog_id y el ratio (PP-F3-PRD-005).
func (s *Store) UsageStats(ctx context.Context, from, to *time.Time) (*app.UsageStats, error) {
	queryTotal := `
		SELECT
			COALESCE(count(*), 0) AS total,
			COALESCE(count(*) FILTER (WHERE (elem->>'catalog_id') IS NOT NULL AND (elem->>'catalog_id') <> ''), 0) AS with_cat
		FROM read_clinical.encounters e,
		     jsonb_array_elements(COALESCE(e.plan->'medications', '[]'::jsonb)) AS elem
		WHERE e.status = 'signed'
		  AND ($1::timestamptz IS NULL OR e.started_at >= $1)
		  AND ($2::timestamptz IS NULL OR e.started_at <= $2)
	`
	var total, withCat int
	err := s.pool.QueryRow(ctx, queryTotal, from, to).Scan(&total, &withCat)
	if err != nil {
		return nil, err
	}

	ratio := 0.0
	if total > 0 {
		ratio = float64(withCat) / float64(total)
	}

	// Top medicamentos por uso
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, usage_count
		FROM ref.medications
		WHERE usage_count > 0
		ORDER BY usage_count DESC, name ASC
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var top []app.MedicationUsage
	for rows.Next() {
		var u app.MedicationUsage
		if err := rows.Scan(&u.CatalogID, &u.Name, &u.Count); err != nil {
			return nil, err
		}
		top = append(top, u)
	}

	return &app.UsageStats{
		ItemsTotal:    total,
		WithCatalogID: withCat,
		Ratio:         ratio,
		Top:           top,
	}, nil
}
