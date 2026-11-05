package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
)

type memoryAdminRepo struct {
	jobs map[uuid.UUID]string
}

func (m *memoryAdminRepo) CreateReindexJob(ctx context.Context, id uuid.UUID, alias, fromIndex, toIndex string, startedBy *uuid.UUID, reason string, startedAt time.Time) error {
	if m.jobs == nil {
		m.jobs = make(map[uuid.UUID]string)
	}
	m.jobs[id] = "started"
	return nil
}

func (m *memoryAdminRepo) CompleteReindexJob(ctx context.Context, id uuid.UUID, finishedAt time.Time) error {
	m.jobs[id] = "completed"
	return nil
}

func (m *memoryAdminRepo) RetryFailedIndexQueue(ctx context.Context) (int64, error) {
	return 0, nil
}

func TestAdminService_ARQ003_ReindexWithoutDowntime(t *testing.T) {
	ctx := context.Background()
	mockOS := app.NewMockClient()
	repo := &memoryAdminRepo{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSvc := app.NewAdminService(mockOS, repo, nil, logger)

	// Inicializar índices v1
	if err := adminSvc.InitIndices(ctx); err != nil {
		t.Fatalf("InitIndices error: %v", err)
	}

	// Verificar estado inicial: alias clinical -> clinical-v1
	aliases, err := mockOS.GetAliases(ctx)
	if err != nil {
		t.Fatalf("GetAliases error: %v", err)
	}
	if len(aliases["clinical"]) != 1 || aliases["clinical"][0] != "clinical-v1" {
		t.Fatalf("expected clinical -> clinical-v1, got %v", aliases["clinical"])
	}

	// Ejecutar reindex
	jobID, err := adminSvc.Reindex(ctx, "clinical", "migración schema v2", nil)
	if err != nil {
		t.Fatalf("Reindex error: %v", err)
	}
	if jobID == uuid.Nil {
		t.Fatal("expected valid job ID")
	}

	// Verificar que el job se completó en repo
	if repo.jobs[jobID] != "completed" {
		t.Fatalf("expected job status 'completed', got %q", repo.jobs[jobID])
	}

	// Verificar alias swap atómico: ahora clinical apunta a clinical-v2
	aliases, err = mockOS.GetAliases(ctx)
	if err != nil {
		t.Fatalf("GetAliases error: %v", err)
	}
	if len(aliases["clinical"]) != 1 || aliases["clinical"][0] != "clinical-v2" {
		t.Fatalf("expected clinical -> clinical-v2, got %v", aliases["clinical"])
	}

	// Verificar que write alias clinical-write ahora apunta a clinical-v2 (y ya no a clinical-v1)
	writeTargets := aliases["clinical-write"]
	foundV2 := false
	for _, target := range writeTargets {
		if target == "clinical-v2" {
			foundV2 = true
		}
		if target == "clinical-v1" {
			t.Fatal("expected clinical-v1 removed from write alias")
		}
	}
	if !foundV2 {
		t.Fatalf("expected clinical-v2 in clinical-write, got %v", writeTargets)
	}
}

func TestAdminService_ARQ003_MappingCheck(t *testing.T) {
	ctx := context.Background()
	mockOS := app.NewMockClient()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSvc := app.NewAdminService(mockOS, nil, nil, logger)

	// Sin índices creados
	reports, err := adminSvc.MappingCheck(ctx)
	if err != nil {
		t.Fatalf("MappingCheck error: %v", err)
	}
	if len(reports) != 4 {
		t.Fatalf("expected 4 reports, got %d", len(reports))
	}
	for _, r := range reports {
		if r.Drift {
			t.Errorf("expected no drift on uninitialized index, got %v", r)
		}
	}
}
