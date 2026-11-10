package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
)

// ReadPrescriptionSummary es el DTO para listas de recetas (PP-F4-API-002, API-004).
type ReadPrescriptionSummary = app.ReadPrescriptionSummary

// ReadPrescriptionDetail es el DTO para el detalle completo de una receta.
type ReadPrescriptionDetail = app.ReadPrescriptionDetail

// Queries maneja las consultas sobre el esquema read_prescriptions.
type Queries struct {
	pool *db.Pool
}

var _ app.PrescriptionQueries = (*Queries)(nil)

// NewQueries construye el servicio de consultas de read_prescriptions.
func NewQueries(pool *db.Pool) *Queries {
	return &Queries{pool: pool}
}

// GetByID devuelve la receta completa con sus dispensaciones, imágenes y contador de verificaciones.
func (q *Queries) GetByID(ctx context.Context, id uuid.UUID) (*ReadPrescriptionDetail, error) {
	const sel = `
SELECT id, version, pet_id, guardian_id, country, source, status,
       issuer_id, issuer_org_id, encounter_id, credential, issued_at, signature,
       valid_until, refills_allowed, refills_used, items, items_remaining,
       external_meta, validation, confirmation, has_controlled, pdf_object_key,
       archived_by_guardian, created_at, updated_at
  FROM read_prescriptions.prescriptions
 WHERE id = $1`

	var (
		d          ReadPrescriptionDetail
		country    string
		validUntil *time.Time
		credJSON   []byte
		sigJSON    []byte
		itemsJSON  []byte
		remJSON    []byte
		metaJSON   []byte
		valJSON    []byte
		confJSON   []byte
	)

	err := q.pool.QueryRow(ctx, sel, id).Scan(
		&d.ID, &d.Version, &d.PetID, &d.GuardianID, &country, &d.Source, &d.Status,
		&d.IssuerID, &d.IssuerOrgID, &d.EncounterID, &credJSON, &d.IssuedAt, &sigJSON,
		&validUntil, &d.RefillsAllowed, &d.RefillsUsed, &itemsJSON, &remJSON,
		&metaJSON, &valJSON, &confJSON, &d.HasControlled, &d.PDFObjectKey,
		&d.ArchivedByGuardian, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPrescriptionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query prescription %s: %w", id, err)
	}

	d.Country = strings.TrimSpace(country)
	if validUntil != nil {
		vu := validUntil.Format("2006-01-02")
		d.ValidUntil = &vu
	}

	if len(credJSON) > 0 && string(credJSON) != "null" {
		_ = json.Unmarshal(credJSON, &d.Credential)
	}
	if len(sigJSON) > 0 && string(sigJSON) != "null" {
		_ = json.Unmarshal(sigJSON, &d.Signature)
	}
	if len(itemsJSON) > 0 && string(itemsJSON) != "null" {
		_ = json.Unmarshal(itemsJSON, &d.Items)
	}
	d.ItemsCount = len(d.Items)
	d.ItemsRemaining = make(map[string]float64)
	if len(remJSON) > 0 && string(remJSON) != "null" {
		_ = json.Unmarshal(remJSON, &d.ItemsRemaining)
	}
	if len(metaJSON) > 0 && string(metaJSON) != "null" {
		_ = json.Unmarshal(metaJSON, &d.ExternalMeta)
	}
	if len(valJSON) > 0 && string(valJSON) != "null" {
		_ = json.Unmarshal(valJSON, &d.Validation)
	}
	if len(confJSON) > 0 && string(confJSON) != "null" {
		_ = json.Unmarshal(confJSON, &d.Confirmation)
	}

	// Dispensaciones
	dispRows, err := q.pool.Query(ctx, `
		SELECT order_id, pharmacy_org_id, at, lines, partial
		  FROM read_prescriptions.dispensations
		 WHERE prescription_id = $1 ORDER BY at ASC`, id)
	if err == nil {
		defer dispRows.Close()
		for dispRows.Next() {
			var (
				disp      domain.Dispensation
				linesJSON []byte
			)
			if err := dispRows.Scan(&disp.OrderID, &disp.PharmacyOrgID, &disp.At, &linesJSON, &disp.Partial); err == nil {
				if len(linesJSON) > 0 {
					_ = json.Unmarshal(linesJSON, &disp.Lines)
				}
				d.Dispensations = append(d.Dispensations, disp)
			}
		}
	}

	// Imágenes si es externa
	if d.Source == string(domain.SourceExternal) {
		imgRows, err := q.pool.Query(ctx, `
			SELECT object_key, content_type, page, width, height, sha256
			  FROM read_prescriptions.images
			 WHERE prescription_id = $1 ORDER BY page ASC`, id)
		if err == nil {
			defer imgRows.Close()
			for imgRows.Next() {
				var img domain.ImageRef
				if err := imgRows.Scan(&img.ObjectKey, &img.ContentType, &img.Page, &img.Width, &img.Height, &img.Sha256); err == nil {
					d.Images = append(d.Images, img)
				}
			}
		}
	}

	// Contador de verificaciones
	_ = q.pool.QueryRow(ctx, `SELECT count(*) FROM read_prescriptions.verifications WHERE prescription_id = $1`, id).Scan(&d.Verifications)

	return &d, nil
}

// ListByOrg devuelve las recetas emitidas por una organización o sus veterinarios (PP-F4-API-002).
func (q *Queries) ListByOrg(ctx context.Context, orgID uuid.UUID, status string, petID *uuid.UUID, issuerID *uuid.UUID, limit int) ([]ReadPrescriptionSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var sb strings.Builder
	sb.WriteString(`
SELECT id, version, pet_id, guardian_id, country, source, status,
       issuer_id, issuer_org_id, issued_at, valid_until, refills_allowed, refills_used,
       jsonb_array_length(items), has_controlled, pdf_object_key, archived_by_guardian,
       created_at, updated_at
  FROM read_prescriptions.prescriptions
 WHERE issuer_org_id = $1`)

	args := []any{orgID}
	argIdx := 2

	if status != "" {
		fmt.Fprintf(&sb, " AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if petID != nil {
		fmt.Fprintf(&sb, " AND pet_id = $%d", argIdx)
		args = append(args, *petID)
		argIdx++
	}
	if issuerID != nil {
		fmt.Fprintf(&sb, " AND issuer_id = $%d", argIdx)
		args = append(args, *issuerID)
		argIdx++
	}

	fmt.Fprintf(&sb, " ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := q.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list org prescriptions: %w", err)
	}
	defer rows.Close()

	var result []ReadPrescriptionSummary
	for rows.Next() {
		var (
			s          ReadPrescriptionSummary
			country    string
			validUntil *time.Time
		)
		err := rows.Scan(
			&s.ID, &s.Version, &s.PetID, &s.GuardianID, &country, &s.Source, &s.Status,
			&s.IssuerID, &s.IssuerOrgID, &s.IssuedAt, &validUntil, &s.RefillsAllowed, &s.RefillsUsed,
			&s.ItemsCount, &s.HasControlled, &s.PDFObjectKey, &s.ArchivedByGuardian,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan prescription summary: %w", err)
		}
		s.Country = strings.TrimSpace(country)
		if validUntil != nil {
			vu := validUntil.Format("2006-01-02")
			s.ValidUntil = &vu
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []ReadPrescriptionSummary{}
	}
	return result, nil
}

// ListByGuardian devuelve las recetas del tutor (PP-F4-API-004).
func (q *Queries) ListByGuardian(ctx context.Context, guardianID uuid.UUID, petID *uuid.UUID, status string, source string, limit int) ([]ReadPrescriptionSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var sb strings.Builder
	if status == "archived" {
		sb.WriteString(`
SELECT id, version, pet_id, guardian_id, country, source, status,
       issuer_id, issuer_org_id, issued_at, valid_until, refills_allowed, refills_used,
       jsonb_array_length(items), has_controlled, pdf_object_key, archived_by_guardian,
       created_at, updated_at
  FROM read_prescriptions.prescriptions
 WHERE guardian_id = $1 AND archived_by_guardian = true`)
	} else {
		sb.WriteString(`
SELECT id, version, pet_id, guardian_id, country, source, status,
       issuer_id, issuer_org_id, issued_at, valid_until, refills_allowed, refills_used,
       jsonb_array_length(items), has_controlled, pdf_object_key, archived_by_guardian,
       created_at, updated_at
  FROM read_prescriptions.prescriptions
 WHERE guardian_id = $1 AND archived_by_guardian = false`)
	}

	args := []any{guardianID}
	argIdx := 2

	if petID != nil {
		fmt.Fprintf(&sb, " AND pet_id = $%d", argIdx)
		args = append(args, *petID)
		argIdx++
	}
	if status != "" && status != "archived" {
		fmt.Fprintf(&sb, " AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if source != "" {
		fmt.Fprintf(&sb, " AND source = $%d", argIdx)
		args = append(args, source)
		argIdx++
	}

	fmt.Fprintf(&sb, " ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := q.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list guardian prescriptions: %w", err)
	}
	defer rows.Close()

	var result []ReadPrescriptionSummary
	for rows.Next() {
		var (
			s          ReadPrescriptionSummary
			country    string
			validUntil *time.Time
		)
		err := rows.Scan(
			&s.ID, &s.Version, &s.PetID, &s.GuardianID, &country, &s.Source, &s.Status,
			&s.IssuerID, &s.IssuerOrgID, &s.IssuedAt, &validUntil, &s.RefillsAllowed, &s.RefillsUsed,
			&s.ItemsCount, &s.HasControlled, &s.PDFObjectKey, &s.ArchivedByGuardian,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan guardian prescription summary: %w", err)
		}
		s.Country = strings.TrimSpace(country)
		if validUntil != nil {
			vu := validUntil.Format("2006-01-02")
			s.ValidUntil = &vu
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []ReadPrescriptionSummary{}
	}
	return result, nil
}

// ListByEncounter devuelve recetas originadas en un encuentro clínico específico.
func (q *Queries) ListByEncounter(ctx context.Context, encounterID uuid.UUID) ([]ReadPrescriptionSummary, error) {
	const query = `
SELECT id, version, pet_id, guardian_id, country, source, status,
       issuer_id, issuer_org_id, issued_at, valid_until, refills_allowed, refills_used,
       jsonb_array_length(items), has_controlled, pdf_object_key, archived_by_guardian,
       created_at, updated_at
  FROM read_prescriptions.prescriptions
 WHERE encounter_id = $1
 ORDER BY created_at DESC`

	rows, err := q.pool.Query(ctx, query, encounterID)
	if err != nil {
		return nil, fmt.Errorf("list encounter prescriptions: %w", err)
	}
	defer rows.Close()

	var result []ReadPrescriptionSummary
	for rows.Next() {
		var (
			s          ReadPrescriptionSummary
			country    string
			validUntil *time.Time
		)
		err := rows.Scan(
			&s.ID, &s.Version, &s.PetID, &s.GuardianID, &country, &s.Source, &s.Status,
			&s.IssuerID, &s.IssuerOrgID, &s.IssuedAt, &validUntil, &s.RefillsAllowed, &s.RefillsUsed,
			&s.ItemsCount, &s.HasControlled, &s.PDFObjectKey, &s.ArchivedByGuardian,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan encounter prescription summary: %w", err)
		}
		s.Country = strings.TrimSpace(country)
		if validUntil != nil {
			vu := validUntil.Format("2006-01-02")
			s.ValidUntil = &vu
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []ReadPrescriptionSummary{}
	}
	return result, nil
}

// Archive marca una receta como archivada para el tutor.
func (q *Queries) Archive(ctx context.Context, id uuid.UUID, guardianID uuid.UUID) error {
	tag, err := q.pool.Exec(ctx, `
		UPDATE read_prescriptions.prescriptions
		   SET archived_by_guardian = true, updated_at = now()
		 WHERE id = $1 AND guardian_id = $2`, id, guardianID)
	if err != nil {
		return fmt.Errorf("archive prescription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPrescriptionNotFound
	}
	return nil
}

// RecordVerification registra un intento de verificación pública (PP-F4-DAT-001, ARQ-011).
func (q *Queries) RecordVerification(ctx context.Context, id uuid.UUID, ipHash []byte, via string) error {
	if via == "" {
		via = "public"
	}
	_, err := q.pool.Exec(ctx, `
		INSERT INTO read_prescriptions.verifications (prescription_id, at, ip_hash, via)
		VALUES ($1, now(), $2, $3)`, id, ipHash, via)
	return err
}

// SetPDFObjectKey actualiza la clave del PDF generado.
func (q *Queries) SetPDFObjectKey(ctx context.Context, id uuid.UUID, objectKey string, version int) error {
	_, err := q.pool.Exec(ctx, `
		UPDATE read_prescriptions.prescriptions
		   SET pdf_object_key = $2, pdf_version = $3, updated_at = now()
		 WHERE id = $1`, id, objectKey, version)
	return err
}

// FindExpiredPrescriptions encuentra prescripciones vigentes cuya fecha valid_until ya expiró.
func (q *Queries) FindExpiredPrescriptions(ctx context.Context, today time.Time) ([]uuid.UUID, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id
		  FROM read_prescriptions.prescriptions
		 WHERE status IN ('issued', 'confirmed') AND valid_until < $1`, today.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("find expired: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// GetExtractionJob obtiene el estado del trabajo de análisis de visión para una prescripción.
func (q *Queries) GetExtractionJob(ctx context.Context, id uuid.UUID) (*app.ExtractionJobDTO, error) {
	const sel = `
		SELECT attempt, status, extractor_id, legibility, warnings, error
		  FROM read_prescriptions.extraction_jobs
		 WHERE prescription_id = $1
		 ORDER BY attempt DESC
		 LIMIT 1`

	var (
		dto          app.ExtractionJobDTO
		warningsJSON []byte
	)
	dto.PrescriptionID = id

	err := q.pool.QueryRow(ctx, sel, id).Scan(
		&dto.Attempt, &dto.Status, &dto.ExtractorID, &dto.Legibility, &warningsJSON, &dto.Reason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPrescriptionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query extraction job %s: %w", id, err)
	}

	if len(warningsJSON) > 0 && string(warningsJSON) != "null" {
		_ = json.Unmarshal(warningsJSON, &dto.Warnings)
	}

	return &dto, nil
}

// GetImage devuelve la referencia a la imagen para una página dada de la receta externa.
func (q *Queries) GetImage(ctx context.Context, id uuid.UUID, page int) (*domain.ImageRef, error) {
	const sel = `
		SELECT object_key, content_type, page, width, height, sha256
		  FROM read_prescriptions.images
		 WHERE prescription_id = $1 AND page = $2`

	var img domain.ImageRef
	err := q.pool.QueryRow(ctx, sel, id, page).Scan(
		&img.ObjectKey, &img.ContentType, &img.Page, &img.Width, &img.Height, &img.Sha256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPrescriptionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query prescription image %s p%d: %w", id, page, err)
	}
	return &img, nil
}

// AddImage persiste una nueva página de imagen en read_prescriptions.images.
func (q *Queries) AddImage(ctx context.Context, id uuid.UUID, img domain.ImageRef) error {
	const ins = `
		INSERT INTO read_prescriptions.images (
			prescription_id, page, object_key, content_type, width, height, sha256, scan_status, uploaded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', now())
		ON CONFLICT (prescription_id, page) DO UPDATE SET
			object_key = EXCLUDED.object_key,
			content_type = EXCLUDED.content_type,
			width = EXCLUDED.width,
			height = EXCLUDED.height,
			sha256 = EXCLUDED.sha256,
			uploaded_at = now()`

	_, err := q.pool.Exec(ctx, ins, id, img.Page, img.ObjectKey, img.ContentType, img.Width, img.Height, img.Sha256)
	return err
}

// CountExternalPrescriptionsToday cuenta cuántas recetas externas ha subido el tutor desde una fecha dada (PP-F4-SEC-010).
func (q *Queries) CountExternalPrescriptionsToday(ctx context.Context, guardianID uuid.UUID, since time.Time) (int, error) {
	const countQuery = `
		SELECT count(*)
		  FROM read_prescriptions.prescriptions
		 WHERE guardian_id = $1 AND source = 'external' AND created_at >= $2`

	var count int
	err := q.pool.QueryRow(ctx, countQuery, guardianID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count external prescriptions: %w", err)
	}
	return count, nil
}
