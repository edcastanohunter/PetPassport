package main

import (
	"context"
	"log/slog"
	"time"

	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
)

// Mantenimiento diario de F2: particiones de audit_log (DAT-008) y purga de
// adjuntos pendientes de adenda huérfanos (> 24 h).
const (
	maintenanceEvery = 24 * time.Hour
	orphanUploadsAge = 24 * time.Hour
)

// clinicalRunners compone los runners de Clinical Records (M2.1): documentos
// PDF (INF-004) y mantenimiento. Sin storage o sin Typst el runner de
// documentos no arranca (los trabajos quedan pending y se procesan cuando
// se despliegue con ambos); se avisa en el log.
func clinicalRunners(ctx context.Context, a *app, logger *slog.Logger) ([]worker.Runner, error) {
	var objects storage.ObjectStorage
	if a.cfg.Storage.Endpoint != "" {
		o, err := storage.NewMinIO(ctx, storage.MinIOOptions{
			Endpoint: a.cfg.Storage.Endpoint, Bucket: a.cfg.Storage.Bucket,
			AccessKey: a.cfg.Storage.AccessKey, SecretKey: a.cfg.Storage.SecretKey, UseSSL: a.cfg.Storage.UseSSL,
		})
		if err != nil {
			return nil, err
		}
		objects = o
	}
	uploads := clinicalinfra.NewUploads(a.pool.Pool)
	runners := []worker.Runner{{Name: "clinical-maintenance", Run: func(ctx context.Context) error {
		t := time.NewTicker(maintenanceEvery)
		defer t.Stop()
		for {
			if err := clinicalinfra.EnsureAuditPartitions(ctx, a.pool.Pool, time.Now()); err != nil {
				logger.ErrorContext(ctx, "audit_log: no se pudieron crear particiones", "error", err)
			}
			keys, err := uploads.PurgeOrphans(ctx, orphanUploadsAge)
			if err != nil {
				logger.ErrorContext(ctx, "clinical: purga de adjuntos pendientes", "error", err)
			}
			for _, k := range keys {
				if objects != nil {
					if err := objects.Delete(ctx, k); err != nil {
						logger.WarnContext(ctx, "clinical: no se pudo borrar adjunto huérfano", "error", err)
					}
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}
		}
	}}}

	typst := pdfrender.NewTypst(a.cfg.TypstBin, pdfrender.DefaultConcurrency, pdfrender.DefaultTimeout)
	switch {
	case objects == nil:
		logger.Warn("PP_STORAGE_ENDPOINT vacío: generación de PDF deshabilitada")
	case typst.Available() != nil:
		logger.Warn("binario typst no encontrado: generación de PDF deshabilitada", "bin", a.cfg.TypstBin)
	default:
		pdf := clinicalinfra.NewPDFWorker(a.pool.Pool, objects, typst, a.cfg.PublicBaseURL, a.tel.Registry, logger)
		runners = append(runners, worker.Runner{Name: "documents", Run: pdf.Run})
	}
	return runners, nil
}
