package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

var (
	metricBulkIndexed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "search",
		Name:      "bulk_indexed_total",
		Help:      "Total de documentos indexados en OpenSearch por índice.",
	}, []string{"index"})

	metricBulkErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "search",
		Name:      "bulk_errors_total",
		Help:      "Total de errores en operaciones bulk de búsqueda.",
	}, []string{"index"})
)

// DocumentSource define la interfaz para cargar documentos antes de indexarlos.
type DocumentSource interface {
	LoadPetDocs(ctx context.Context, petIDs []uuid.UUID) ([]domain.PetDoc, error)
	LoadClinicalDocs(ctx context.Context, docIDs []uuid.UUID) ([]domain.ClinicalDoc, error)
}

// Indexer coordina la indexación idempotente en OpenSearch (ARQ-001).
type Indexer struct {
	client Client
	source DocumentSource
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewIndexer crea un nuevo Indexer.
func NewIndexer(client Client, source DocumentSource, pool *pgxpool.Pool, logger *slog.Logger) *Indexer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Indexer{
		client: client,
		source: source,
		pool:   pool,
		logger: logger,
	}
}

// ProcessBulkPets procesa un lote de IDs de mascotas cargándolos y enviándolos en bulk con version_type=external_gte (ARQ-001).
func (idx *Indexer) ProcessBulkPets(ctx context.Context, petIDs []string) error {
	var uuids []uuid.UUID
	for _, idStr := range petIDs {
		if u, err := uuid.Parse(idStr); err == nil {
			uuids = append(uuids, u)
		}
	}
	if len(uuids) == 0 {
		return nil
	}

	docs, err := idx.source.LoadPetDocs(ctx, uuids)
	if err != nil {
		return fmt.Errorf("loading pet docs: %w", err)
	}

	var ops []BulkOperation
	docVersions := make(map[string]int64)

	for _, doc := range docs {
		ops = append(ops, BulkOperation{
			Action:      "index",
			Index:       domain.AliasPetsWrite,
			ID:          doc.PetID,
			Version:     doc.Version,
			VersionType: "external_gte",
			Document:    doc,
		})
		docVersions[doc.PetID] = doc.Version
	}

	if len(ops) > 0 {
		resp, err := idx.client.Bulk(ctx, ops)
		if err != nil {
			metricBulkErrors.WithLabelValues(domain.IndexPets).Inc()
			return fmt.Errorf("bulk pets: %w", err)
		}
		if resp.Errors {
			for _, item := range resp.Items {
				for _, opResp := range item {
					if opResp.Error != nil && opResp.Status != 409 { // 409 es conflicto de versión external_gte esperado
						idx.logger.WarnContext(ctx, "bulk pet error", "id", opResp.ID, "reason", opResp.Error.Reason)
						metricBulkErrors.WithLabelValues(domain.IndexPets).Inc()
					}
				}
			}
		}
		metricBulkIndexed.WithLabelValues(domain.IndexPets).Add(float64(len(ops)))
	}

	// Limpiar filas procesadas de search.index_queue
	if idx.pool != nil {
		for docID, ver := range docVersions {
			_, _ = idx.pool.Exec(ctx, `
				DELETE FROM search.index_queue
				WHERE index = 'pets' AND doc_id = $1 AND version <= $2
			`, docID, ver)
		}
	}

	return nil
}

// ProcessBulkClinical procesa un lote de IDs de encuentros/carnet.
func (idx *Indexer) ProcessBulkClinical(ctx context.Context, docIDs []string) error {
	var uuids []uuid.UUID
	for _, idStr := range docIDs {
		if u, err := uuid.Parse(idStr); err == nil {
			uuids = append(uuids, u)
		}
	}
	if len(uuids) == 0 {
		return nil
	}

	docs, err := idx.source.LoadClinicalDocs(ctx, uuids)
	if err != nil {
		return fmt.Errorf("loading clinical docs: %w", err)
	}

	var ops []BulkOperation
	docVersions := make(map[string]int64)

	for _, doc := range docs {
		ops = append(ops, BulkOperation{
			Action:      "index",
			Index:       domain.AliasClinicalWrite,
			ID:          doc.PetID, // o id del encuentro
			Version:     doc.Position,
			VersionType: "external_gte",
			Document:    doc,
		})
		docVersions[doc.PetID] = doc.Position
	}

	if len(ops) > 0 {
		resp, err := idx.client.Bulk(ctx, ops)
		if err != nil {
			metricBulkErrors.WithLabelValues(domain.IndexClinical).Inc()
			return fmt.Errorf("bulk clinical: %w", err)
		}
		if resp.Errors {
			for _, item := range resp.Items {
				for _, opResp := range item {
					if opResp.Error != nil && opResp.Status != 409 {
						idx.logger.WarnContext(ctx, "bulk clinical error", "id", opResp.ID, "reason", opResp.Error.Reason)
						metricBulkErrors.WithLabelValues(domain.IndexClinical).Inc()
					}
				}
			}
		}
		metricBulkIndexed.WithLabelValues(domain.IndexClinical).Add(float64(len(ops)))
	}

	// Limpiar filas procesadas
	if idx.pool != nil {
		for docID, ver := range docVersions {
			_, _ = idx.pool.Exec(ctx, `
				DELETE FROM search.index_queue
				WHERE index = 'clinical' AND doc_id = $1 AND version <= $2
			`, docID, ver)
		}
	}

	return nil
}

// EnqueueIndexDoc añade un documento a search.index_queue (ARQ-001).
func (idx *Indexer) EnqueueIndexDoc(ctx context.Context, indexName, docID string, petID *uuid.UUID, version int64) error {
	if idx.pool == nil {
		return nil
	}
	_, err := idx.pool.Exec(ctx, `
		INSERT INTO search.index_queue (index, doc_id, pet_id, version, status, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		ON CONFLICT (index, doc_id) DO UPDATE SET
			version = GREATEST(search.index_queue.version, EXCLUDED.version),
			status = 'pending',
			updated_at = EXCLUDED.updated_at
	`, indexName, docID, petID, version, time.Now())
	return err
}
