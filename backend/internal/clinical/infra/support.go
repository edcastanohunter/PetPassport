package infra

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ------------------------------------------------------------- frescura

// FreshnessReader implementa app.Freshness: segundos desde el evento más
// antiguo aún no aplicado por la proyección (0 si está al día).
type FreshnessReader struct{ pool *pgxpool.Pool }

var _ app.Freshness = (*FreshnessReader)(nil)

// NewFreshness construye el lector de frescura.
func NewFreshness(pool *pgxpool.Pool) *FreshnessReader { return &FreshnessReader{pool: pool} }

// LagSeconds devuelve el retraso de la proyección.
func (f *FreshnessReader) LagSeconds(ctx context.Context, projection string) (float64, error) {
	var lag float64
	err := f.pool.QueryRow(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(e.recorded_at)), 0)::float8
		FROM es.events e
		WHERE e.global_position > COALESCE(
			(SELECT last_position FROM platform.projection_checkpoints WHERE projection_name = $1), 0)`,
		projection).Scan(&lag)
	if lag < 0 {
		lag = 0
	}
	return lag, err
}

// ------------------------------------------------------------ documentos

// DocumentStore implementa app.Documents sobre read_clinical.document_jobs.
type DocumentStore struct{ pool *pgxpool.Pool }

var _ app.Documents = (*DocumentStore)(nil)

// NewDocuments construye el lector de trabajos de documentos.
func NewDocuments(pool *pgxpool.Pool) *DocumentStore { return &DocumentStore{pool: pool} }

const documentColumns = `id, kind, ref_id, version, status, COALESCE(object_key, ''), updated_at`

func scanDocument(row pgx.Row) (app.DocumentJobView, error) {
	var j app.DocumentJobView
	err := row.Scan(&j.ID, &j.Kind, &j.RefID, &j.Version, &j.Status, &j.ObjectKey, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.DocumentJobView{}, &kernel.NotFoundError{Resource: "document"}
	}
	return j, err
}

// Job devuelve un trabajo por id.
func (d *DocumentStore) Job(ctx context.Context, id uuid.UUID) (app.DocumentJobView, error) {
	return scanDocument(d.pool.QueryRow(ctx,
		`SELECT `+documentColumns+` FROM read_clinical.document_jobs WHERE id = $1`, id))
}

// LatestForRef devuelve el trabajo de mayor versión del recurso.
func (d *DocumentStore) LatestForRef(ctx context.Context, kind string, refID uuid.UUID) (app.DocumentJobView, error) {
	return scanDocument(d.pool.QueryRow(ctx, `SELECT `+documentColumns+`
		FROM read_clinical.document_jobs WHERE kind = $1 AND ref_id = $2
		ORDER BY version DESC LIMIT 1`, kind, refID))
}

// ----------------------------------------------------- adjuntos en staging

// UploadStore implementa app.StagedUploads sobre read_clinical.attachment_uploads.
type UploadStore struct{ pool *pgxpool.Pool }

var _ app.StagedUploads = (*UploadStore)(nil)

// NewUploads construye el almacén de adjuntos pendientes de adenda.
func NewUploads(pool *pgxpool.Pool) *UploadStore { return &UploadStore{pool: pool} }

// Stage registra un adjunto subido a una consulta firmada.
func (u *UploadStore) Stage(ctx context.Context, s app.StagedUpload) error {
	a := s.Attachment
	_, err := u.pool.Exec(ctx, `
		INSERT INTO read_clinical.attachment_uploads
		  (id, encounter_id, uploaded_by, object_key, content_type, size_bytes, description, visible_to_guardian, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)`,
		a.ID, s.EncounterID, s.UploadedBy, a.ObjectKey, string(a.ContentType), a.SizeBytes,
		a.Description, a.VisibleToGuardian, s.CreatedAt)
	return err
}

const uploadColumns = `id, encounter_id, uploaded_by, object_key, content_type, size_bytes,
	COALESCE(description, ''), visible_to_guardian, created_at`

func scanUpload(row pgx.Row) (app.StagedUpload, error) {
	var s app.StagedUpload
	var ct string
	err := row.Scan(&s.Attachment.ID, &s.EncounterID, &s.UploadedBy, &s.Attachment.ObjectKey, &ct,
		&s.Attachment.SizeBytes, &s.Attachment.Description, &s.Attachment.VisibleToGuardian, &s.CreatedAt)
	s.Attachment.ContentType = domain.ContentType(ct)
	return s, err
}

// Pending lista los adjuntos pendientes del encuentro.
func (u *UploadStore) Pending(ctx context.Context, encounterID uuid.UUID) ([]app.StagedUpload, error) {
	rows, err := u.pool.Query(ctx, `SELECT `+uploadColumns+`
		FROM read_clinical.attachment_uploads WHERE encounter_id = $1 ORDER BY created_at, id`, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.StagedUpload
	for rows.Next() {
		s, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get devuelve un adjunto pendiente del encuentro.
func (u *UploadStore) Get(ctx context.Context, encounterID, attachmentID uuid.UUID) (app.StagedUpload, error) {
	s, err := scanUpload(u.pool.QueryRow(ctx, `SELECT `+uploadColumns+`
		FROM read_clinical.attachment_uploads WHERE encounter_id = $1 AND id = $2`, encounterID, attachmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.StagedUpload{}, &kernel.NotFoundError{Resource: "attachment"}
	}
	return s, err
}

// Delete borra los adjuntos pendientes indicados (incorporados o descartados).
func (u *UploadStore) Delete(ctx context.Context, encounterID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := u.pool.Exec(ctx, `DELETE FROM read_clinical.attachment_uploads
		WHERE encounter_id = $1 AND id = ANY($2)`, encounterID, ids)
	return err
}

// PurgeOrphans borra los pendientes más antiguos que maxAge y devuelve sus
// claves de objeto para eliminarlas del storage (worker).
func (u *UploadStore) PurgeOrphans(ctx context.Context, maxAge time.Duration) ([]string, error) {
	rows, err := u.pool.Query(ctx, `DELETE FROM read_clinical.attachment_uploads
		WHERE created_at < now() - make_interval(secs => $1) RETURNING object_key`, maxAge.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ---------------------------------------------------------- auditoría

// ReadAuditor implementa app.AccessAuditor (SEC-005): una entrada
// `clinical.read` por request en su propia transacción corta.
type ReadAuditor struct{ pool *pgxpool.Pool }

var _ app.AccessAuditor = (*ReadAuditor)(nil)

// NewReadAuditor construye el auditor de lecturas clínicas.
func NewReadAuditor(pool *pgxpool.Pool) *ReadAuditor { return &ReadAuditor{pool: pool} }

// AuditActionClinicalRead es la acción registrada en audit_log.
const AuditActionClinicalRead = "clinical.read"

// RecordRead inserta la entrada (sin PII: solo identificadores).
func (a *ReadAuditor) RecordRead(ctx context.Context, e app.ReadAudit) error {
	details := map[string]any{"pet_id": e.PetID.String(), "resource": e.Resource}
	if e.OrganizationID != nil {
		details["organization_id"] = e.OrganizationID.String()
	}
	entry := audit.Entry{
		ActorID: &e.ActorID, ActorRoles: e.ActorRoles, Action: AuditActionClinicalRead,
		TargetType: e.Resource, TargetID: &e.ResourceID, Reason: e.Reason, Details: details,
	}
	if id, err := uuid.Parse(e.CorrelationID); err == nil {
		entry.CorrelationID = &id
	}
	return pgx.BeginFunc(ctx, a.pool, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, entry)
	})
}

// EnsureAuditPartitions crea las particiones mensuales de audit_log del mes
// actual y el siguiente (DAT-008). Lo invoca el worker.
func EnsureAuditPartitions(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	for _, m := range []time.Time{now, now.AddDate(0, 1, 0)} {
		if _, err := pool.Exec(ctx, `SELECT platform.ensure_audit_partition($1::date)`, m.UTC().Format("2006-01-02")); err != nil {
			return err
		}
	}
	return nil
}
