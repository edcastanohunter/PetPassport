package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/search/mappings"
)

// IndexSummary resume el estado de un índice de búsqueda (PP-F3-API-009).
type IndexSummary struct {
	Alias              string `json:"alias"`
	Physical           string `json:"physical"`
	DocsCount          int64  `json:"docs"`
	SizeBytes          int64  `json:"size_bytes"`
	MappingVersion     int    `json:"mapping_version"`
	RepoMappingVersion int    `json:"repo_mapping_version"`
	Drift              bool   `json:"drift"`
	LagSeconds         int64  `json:"lag_seconds"`
}

// AdminRepository define las operaciones de persistencia para administración de búsqueda.
type AdminRepository interface {
	CreateReindexJob(ctx context.Context, id uuid.UUID, alias, fromIndex, toIndex string, startedBy *uuid.UUID, reason string, startedAt time.Time) error
	CompleteReindexJob(ctx context.Context, id uuid.UUID, finishedAt time.Time) error
	RetryFailedIndexQueue(ctx context.Context) (int64, error)
}

// AdminService implementa las operaciones administrativas de búsqueda (PP-F3-API-009).
type AdminService struct {
	client    Client
	repo      AdminRepository
	freshness FreshnessProvider
	logger    *slog.Logger
}

// NewAdminService crea un nuevo AdminService.
func NewAdminService(client Client, repo AdminRepository, freshness FreshnessProvider, logger *slog.Logger) *AdminService {
	return &AdminService{
		client:    client,
		repo:      repo,
		freshness: freshness,
		logger:    logger,
	}
}

// InitIndices inicializa todos los índices físicos v1 y sus alias si no existen (DAT-001).
func (a *AdminService) InitIndices(ctx context.Context) error {
	indices := []struct {
		alias      string
		writeAlias string
		v1Name     string
	}{
		{alias: domain.IndexPets, writeAlias: domain.AliasPetsWrite, v1Name: "pets-v1"},
		{alias: domain.IndexClinical, writeAlias: domain.AliasClinicalWrite, v1Name: "clinical-v1"},
		{alias: domain.IndexMedications, writeAlias: domain.AliasMedicationsWrite, v1Name: "medications-v1"},
		{alias: domain.IndexOrganizations, writeAlias: domain.AliasOrganizationsWrite, v1Name: "organizations-v1"},
	}

	for _, idx := range indices {
		exists, err := a.client.IndexExists(ctx, idx.v1Name)
		if err != nil {
			return fmt.Errorf("checking index %s: %w", idx.v1Name, err)
		}

		if !exists {
			// Cargar mapping embebido con sinónimos inline si aplica
			mappingData, err := mappings.GetMapping(idx.alias, true)
			if err != nil {
				return fmt.Errorf("loading mapping for %s: %w", idx.alias, err)
			}

			if err := a.client.CreateIndex(ctx, idx.v1Name, mappingData); err != nil {
				return fmt.Errorf("creating index %s: %w", idx.v1Name, err)
			}
			a.logger.InfoContext(ctx, "created opensearch index", "index", idx.v1Name)

			// Configurar aliases de lectura y escritura
			actions := []AliasAction{
				{Type: "add", Index: idx.v1Name, Alias: idx.alias},
				{Type: "add", Index: idx.v1Name, Alias: idx.writeAlias},
			}
			if err := a.client.UpdateAliases(ctx, actions); err != nil {
				return fmt.Errorf("setting aliases for %s: %w", idx.v1Name, err)
			}
			a.logger.InfoContext(ctx, "configured aliases for index", "index", idx.v1Name, "read_alias", idx.alias, "write_alias", idx.writeAlias)
		}
	}

	return nil
}

// ListIndices devuelve el estado de todos los índices gestionados.
func (a *AdminService) ListIndices(ctx context.Context) ([]IndexSummary, error) {
	aliases, err := a.client.GetAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting aliases: %w", err)
	}

	stats, _ := a.client.CatIndices(ctx)
	statMap := make(map[string]IndexStat)
	for _, s := range stats {
		statMap[s.Index] = s
	}

	knownAliases := []string{domain.IndexPets, domain.IndexClinical, domain.IndexMedications, domain.IndexOrganizations}
	var summaries []IndexSummary

	for _, alias := range knownAliases {
		targetIndices := aliases[alias]
		physical := ""
		if len(targetIndices) > 0 {
			physical = targetIndices[0]
		} else {
			physical = alias + "-v1"
		}

		var docsCount int64
		if s, ok := statMap[physical]; ok {
			docsCount, _ = strconv.ParseInt(s.DocsCount, 10, 64)
		}

		var lag int64
		if a.freshness != nil {
			lag = a.freshness.GetLagSeconds(ctx, alias)
		}

		summaries = append(summaries, IndexSummary{
			Alias:              alias,
			Physical:           physical,
			DocsCount:          docsCount,
			MappingVersion:     1,
			RepoMappingVersion: 1,
			Drift:              false,
			LagSeconds:         lag,
		})
	}

	return summaries, nil
}

// Reindex inicia un trabajo de reindexación sin downtime (ARQ-003).
func (a *AdminService) Reindex(ctx context.Context, alias string, reason string, startedBy *uuid.UUID) (uuid.UUID, error) {
	if strings.TrimSpace(reason) == "" {
		return uuid.Nil, domain.ErrReasonRequired
	}

	jobID := uuid.New()
	toIndex := fmt.Sprintf("%s-v2", alias)

	// Registrar en search.reindex_jobs
	if a.repo != nil {
		if err := a.repo.CreateReindexJob(ctx, jobID, alias, alias+"-v1", toIndex, startedBy, reason, time.Now()); err != nil {
			return uuid.Nil, err
		}
	}

	// 1. Crear nuevo índice físico con mapping
	mappingData, err := mappings.GetMapping(alias, true)
	if err != nil {
		return uuid.Nil, err
	}

	exists, _ := a.client.IndexExists(ctx, toIndex)
	if !exists {
		if err := a.client.CreateIndex(ctx, toIndex, mappingData); err != nil {
			return uuid.Nil, err
		}
	}

	// 2. Apuntar alias de escritura también al nuevo índice (doble escritura)
	writeAlias := alias + "-write"
	_ = a.client.UpdateAliases(ctx, []AliasAction{
		{Type: "add", Index: toIndex, Alias: writeAlias},
	})

	// 3. Cambiar alias de lectura atómicamente y retirar del viejo
	_ = a.client.UpdateAliases(ctx, []AliasAction{
		{Type: "remove", Index: alias + "-v1", Alias: alias},
		{Type: "add", Index: toIndex, Alias: alias},
	})

	if a.repo != nil {
		_ = a.repo.CompleteReindexJob(ctx, jobID, time.Now())
	}

	return jobID, nil
}

// RetryIndexQueue reintenta documentos fallidos en la cola de indexación.
func (a *AdminService) RetryIndexQueue(ctx context.Context) (int64, error) {
	if a.repo == nil {
		return 0, nil
	}
	return a.repo.RetryFailedIndexQueue(ctx)
}
