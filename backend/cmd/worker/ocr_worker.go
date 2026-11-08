package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/hibiken/asynq"

	ocrapp "github.com/edcastanohunter/petpassport/backend/internal/ocr/app"
	ocrbudget "github.com/edcastanohunter/petpassport/backend/internal/ocr/budget"
	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
	ocrinfra "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra"
	claudeext "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/claude"
	mockext "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/mock"
	tesseractext "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/tesseract"
	ocrmatcher "github.com/edcastanohunter/petpassport/backend/internal/ocr/matcher"
	ocrmetrics "github.com/edcastanohunter/petpassport/backend/internal/ocr/metrics"
	ocrpreprocess "github.com/edcastanohunter/petpassport/backend/internal/ocr/preprocess"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/egress"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/queue"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	prescriptioninfra "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/infra"
)

func registerOCRWorker(asynqSrv *queue.Server, a *app, logger *slog.Logger) {
	reg := a.store.Registry()
	rxRepo := prescriptioninfra.NewRepository(a.store, reg)
	projRunner := projection.NewRunner()
	prescriptioninfra.RegisterSyncProjectors(projRunner, reg)

	var extractor ocrdomain.DocumentExtractor
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		egressClient := egress.NewClient(egress.Config{
			AllowedHosts:   []string{"api.anthropic.com"},
			RequestTimeout: 60 * time.Second,
		})
		cExt, err := claudeext.NewExtractor(claudeext.Config{
			APIKey:     apiKey,
			HTTPClient: egressClient.HTTPClient(),
		})
		if err != nil {
			logger.Warn("claude extractor init failed, using mock", "error", err)
			extractor = mockext.NewMockExtractor(ocrdomain.ExtractionResult{})
		} else {
			extractor = cExt
		}
	} else {
		extractor = mockext.NewMockExtractor(ocrdomain.ExtractionResult{})
	}

	fallbackExt := tesseractext.NewExtractor()
	thresholds := ocrdomain.DefaultThresholds()
	matcher := ocrmatcher.NewMatcher(ocrmatcher.NewPostgresMedicationSearcher(a.pool.Pool), thresholds)
	budgetSvc := ocrbudget.NewService(a.pool.Pool)
	metricsRec := ocrmetrics.NewRecorder(a.pool.Pool)
	jobRepo := ocrinfra.NewJobRepo(a.pool.Pool)

	pipelineSvc := ocrapp.NewPipelineService(ocrapp.PipelineOptions{
		Preprocessor:      ocrpreprocess.NewPreprocessor(),
		Extractor:         extractor,
		FallbackExtractor: fallbackExt,
		Matcher:           matcher,
		Budget:            budgetSvc,
		Metrics:           metricsRec,
		Jobs:              jobRepo,
		Thresholds:        thresholds,
		PrescriptionRepo:  rxRepo,
		UoW:               db.NewUnitOfWork(a.pool),
		Projector:         prescriptioninfra.NewSyncProjector(projRunner),
		Logger:            logger,
	})

	var objects storage.ObjectStorage
	if a.cfg.Storage.Endpoint != "" {
		var err error
		objects, err = storage.NewMinIO(context.Background(), storage.MinIOOptions{
			Endpoint:  a.cfg.Storage.Endpoint,
			Bucket:    a.cfg.Storage.Bucket,
			AccessKey: a.cfg.Storage.AccessKey,
			SecretKey: a.cfg.Storage.SecretKey,
			UseSSL:    a.cfg.Storage.UseSSL,
		})
		if err != nil {
			logger.Warn("storage init failed for ocr worker", "error", err)
		}
	}

	asynqSrv.HandleFunc(queue.TypeOCRPipeline, func(ctx context.Context, t *asynq.Task) error {
		var p queue.OCRPipelinePayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		logger.InfoContext(ctx, "asynq ocr:pipeline received", "prescription_id", p.PrescriptionID, "attempt", p.Attempt)

		rx, err := rxRepo.Load(ctx, p.PrescriptionID)
		if err != nil {
			return err
		}

		var rawPages [][]byte
		if objects != nil {
			if reader, ok := objects.(storage.ObjectReader); ok {
				for _, img := range rx.Images {
					rc, err := reader.GetObject(ctx, img.ObjectKey)
					if err == nil {
						data, _ := io.ReadAll(rc)
						_ = rc.Close()
						if len(data) > 0 {
							rawPages = append(rawPages, data)
						}
					}
				}
			}
		}

		return pipelineSvc.RunPipeline(ctx, ocrapp.ProcessDocumentInput{
			PrescriptionID: p.PrescriptionID,
			Attempt:        p.Attempt,
			RawPages:       rawPages,
			Country:        p.Country,
			PetSpecies:     p.PetSpecies,
			PetName:        p.PetName,
			GuardianName:   p.GuardianName,
		})
	})
}
