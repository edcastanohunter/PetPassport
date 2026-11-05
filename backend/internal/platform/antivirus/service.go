package antivirus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
)

type Storage interface {
	storage.ObjectReader
	storage.ObjectStorage
}

// Service orquesta el escaneo antivirus, la cuarentena de archivos infectados y el backfill (PP-F3-ARQ-014, PP-F3-SEC-005).
type Service struct {
	pool    *pgxpool.Pool
	storage Storage
	scanner Scanner
	logger  *slog.Logger
}

func NewService(pool *pgxpool.Pool, st Storage, scanner Scanner, logger *slog.Logger) *Service {
	if scanner == nil {
		scanner = NewInProcessScanner()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		pool:    pool,
		storage: st,
		scanner: scanner,
		logger:  logger,
	}
}

// ScanAttachment escanea un adjunto específico y actualiza su estado.
func (s *Service) ScanAttachment(ctx context.Context, encounterID, attachmentID uuid.UUID, objectKey string) (Result, error) {
	if s.storage == nil {
		return Result{Status: StatusClean}, nil
	}

	rc, err := s.storage.GetObject(ctx, objectKey)
	if err != nil {
		return Result{Status: StatusPending}, fmt.Errorf("getting object for scan: %w", err)
	}
	defer func() { _ = rc.Close() }()

	res, err := s.scanner.Scan(ctx, rc)
	if err != nil {
		s.logger.ErrorContext(ctx, "antivirus scan error", "key", objectKey, "error", err)
		return Result{Status: StatusPending}, err
	}

	if res.Status == StatusClean {
		if err := s.markClean(ctx, attachmentID); err != nil {
			return res, err
		}
		s.logger.InfoContext(ctx, "attachment scan clean", "attachment_id", attachmentID, "key", objectKey)
		return res, nil
	}

	// Infected
	s.logger.WarnContext(ctx, "attachment INFECTED by malware", "attachment_id", attachmentID, "virus", res.VirusName, "key", objectKey)
	if err := s.quarantine(ctx, encounterID, attachmentID, objectKey, res.VirusName); err != nil {
		s.logger.ErrorContext(ctx, "failed to quarantine attachment", "attachment_id", attachmentID, "error", err)
		return res, err
	}

	return res, nil
}

func (s *Service) markClean(ctx context.Context, attachmentID uuid.UUID) error {
	if s.pool == nil {
		return nil
	}
	_, err1 := s.pool.Exec(ctx, `UPDATE read_clinical.attachment_uploads SET scan_status = 'clean' WHERE id = $1`, attachmentID)
	_, err2 := s.pool.Exec(ctx, `UPDATE read_clinical.encounter_attachments SET scan_status = 'clean' WHERE id = $1`, attachmentID)
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *Service) quarantine(ctx context.Context, encounterID, attachmentID uuid.UUID, objectKey, virusName string) error {
	// 1. Marcar como infected en BD si hay pool disponible
	if s.pool != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE read_clinical.attachment_uploads SET scan_status = 'infected' WHERE id = $1`, attachmentID)
		_, _ = s.pool.Exec(ctx, `UPDATE read_clinical.encounter_attachments SET scan_status = 'infected' WHERE id = $1`, attachmentID)
	}

	// 2. Mover objeto a quarantine/
	quarantineKey := "quarantine/" + objectKey
	if s.storage != nil {
		rc, err := s.storage.GetObject(ctx, objectKey)
		if err == nil {
			_ = s.storage.Put(ctx, quarantineKey, "application/octet-stream", rc, -1)
			_ = rc.Close()
			_ = s.storage.Delete(ctx, objectKey)
		}
	}

	// 3. Crear alerta de seguridad en platform.security_alerts y registro en audit log
	if s.pool != nil {
		alertID := uuid.New()
		details, _ := json.Marshal(map[string]any{
			"attachment_id":  attachmentID,
			"encounter_id":   encounterID,
			"object_key":     objectKey,
			"quarantine_key": quarantineKey,
			"virus_name":     virusName,
			"detected_at":    time.Now().UTC(),
		})
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO platform.security_alerts (id, rule, details, status, created_at)
			VALUES ($1, 'malware_detected', $2, 'open', now())
		`, alertID, details)

		_, _ = s.pool.Exec(ctx, `
			INSERT INTO platform.audit_log (id, occurred_at, action, target_type, target_id, reason, details)
			VALUES ($1, now(), 'attachment.quarantined', 'attachment', $2, 'Infected with malware', $3)
		`, uuid.New(), attachmentID, details)
	}

	return nil
}

// BackfillUnscanned escanea todos los adjuntos que quedaron en estado 'pending'.
func (s *Service) BackfillUnscanned(ctx context.Context) (int, error) {
	if s.pool == nil {
		return 0, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, encounter_id, object_key FROM read_clinical.attachment_uploads WHERE scan_status = 'pending'
		UNION
		SELECT id, encounter_id, object_key FROM read_clinical.encounter_attachments WHERE scan_status = 'pending'
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type pendingItem struct {
		id          uuid.UUID
		encounterID uuid.UUID
		objectKey   string
	}
	var items []pendingItem
	for rows.Next() {
		var it pendingItem
		if err := rows.Scan(&it.id, &it.encounterID, &it.objectKey); err == nil {
			items = append(items, it)
		}
	}

	count := 0
	for _, it := range items {
		_, err := s.ScanAttachment(ctx, it.encounterID, it.id, it.objectKey)
		if err == nil {
			count++
		}
	}
	return count, nil
}
