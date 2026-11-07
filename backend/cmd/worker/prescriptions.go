package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
	prescriptionapp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	prescriptioninfra "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/infra"
)

const rxExpirationInterval = 1 * time.Hour

func prescriptionsRunners(a *app, logger *slog.Logger) ([]worker.Runner, error) {
	reg := a.store.Registry()
	rxRepo := prescriptioninfra.NewRepository(a.store, reg)
	rxQueries := prescriptioninfra.NewQueries(a.pool)
	runner := projection.NewRunner()
	prescriptioninfra.RegisterSyncProjectors(runner, reg)

	rxSvc := prescriptionapp.NewService(prescriptionapp.Options{
		Repo:     rxRepo,
		UoW:      db.NewUnitOfWork(a.pool),
		Projects: prescriptioninfra.NewSyncProjector(runner),
		Queries:  rxQueries,
		Logger:   logger,
	})

	return []worker.Runner{{
		Name: "prescriptions-expiration",
		Run: func(ctx context.Context) error {
			ticker := time.NewTicker(rxExpirationInterval)
			defer ticker.Stop()

			if n, err := rxSvc.ExpirePrescriptions(ctx); err != nil {
				logger.ErrorContext(ctx, "prescriptions: fallo al expirar recetas", "error", err)
			} else if n > 0 {
				logger.InfoContext(ctx, "prescriptions: recetas expiradas", "count", n)
			}

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					if n, err := rxSvc.ExpirePrescriptions(ctx); err != nil {
						logger.ErrorContext(ctx, "prescriptions: fallo al expirar recetas", "error", err)
					} else if n > 0 {
						logger.InfoContext(ctx, "prescriptions: recetas expiradas", "count", n)
					}
				}
			}
		},
	}}, nil
}
