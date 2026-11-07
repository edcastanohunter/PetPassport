package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
	ocrmock "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/mock"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	prescriptiondomain "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
)

type dummyPreprocessor struct {
	page ocrdomain.PreprocessedPage
	err  error
}

func (d *dummyPreprocessor) ProcessPage(ctx context.Context, pageNum int, rawData []byte) (ocrdomain.PreprocessedPage, error) {
	if d.err != nil {
		return ocrdomain.PreprocessedPage{}, d.err
	}
	p := d.page
	p.PageNumber = pageNum
	return p, nil
}

type dummyMatcher struct {
	candidates []ocrdomain.CandidateProduct
}

func (d *dummyMatcher) MatchItem(ctx context.Context, productText, rawText, species string) ([]ocrdomain.CandidateProduct, bool, error) {
	return d.candidates, len(d.candidates) > 0, nil
}

type dummyBudget struct {
	status   ocrdomain.BudgetStatus
	checkErr error
}

func (d *dummyBudget) CheckBudget(ctx context.Context) (ocrdomain.BudgetStatus, error) {
	return d.status, d.checkErr
}

func (d *dummyBudget) RecordUsage(ctx context.Context, day time.Time, provider, model string, inTokens, outTokens int64, costUSD float64, refused bool) error {
	return nil
}

type dummyMetrics struct{}

func (d *dummyMetrics) RecordDiff(ctx context.Context, day time.Time, diff map[string]any, originalItems []ocrdomain.ExtractedItem) error {
	return nil
}

func (d *dummyMetrics) RecordJobStatus(ctx context.Context, day time.Time, status string, latencyMs int) error {
	return nil
}

type dummyJobRepo struct {
	lastStatus string
	errMsg     *string
}

func (d *dummyJobRepo) UpdateJobStatus(ctx context.Context, rxID uuid.UUID, attempt int, status, extractorID string, legibility *float64, warnings []string, resultKey *string, inTokens, outTokens *int, costUSD *float64, latencyMs *int, errMsg *string) error {
	d.lastStatus = status
	d.errMsg = errMsg
	return nil
}

func (d *dummyJobRepo) GetJob(ctx context.Context, rxID uuid.UUID) (ocrdomain.ExtractionJobRecord, error) {
	return ocrdomain.ExtractionJobRecord{Status: d.lastStatus}, nil
}

type memoryRxRepo struct {
	rx *prescriptiondomain.Prescription
}

func (m *memoryRxRepo) Load(ctx context.Context, id uuid.UUID) (*prescriptiondomain.Prescription, error) {
	return m.rx, nil
}

func (m *memoryRxRepo) Save(ctx context.Context, p *prescriptiondomain.Prescription, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	p.MarkCommitted()
	return nil, nil
}

func (m *memoryRxRepo) ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error) {
	return "hash", nil
}

type syncTxRunner struct{}

func (s syncTxRunner) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func setupTestPrescription(rxID, petID, guardianID uuid.UUID) *prescriptiondomain.Prescription {
	rx := prescriptiondomain.NewPrescription()
	images := []prescriptiondomain.ImageRef{
		{ObjectKey: "rx/page-1.jpg", ContentType: "image/jpeg", Page: 1, Sha256: []byte("sha")},
	}
	_, _ = rx.UploadExternalPrescription(rxID, petID, guardianID, images, "CO")
	rx.MarkCommitted()
	return rx
}

func TestPipeline_Success(t *testing.T) {
	rxID := uuid.New()
	petID := uuid.New()
	guardianID := uuid.New()
	rx := setupTestPrescription(rxID, petID, guardianID)
	rxRepo := &memoryRxRepo{rx: rx}
	jobRepo := &dummyJobRepo{}

	prep := &dummyPreprocessor{
		page: ocrdomain.PreprocessedPage{
			QualityScore: 0.90,
			Data:         []byte("preprocessed-img"),
			ContentType:  "image/jpeg",
		},
	}

	extractor := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{
		Extraction: ocrdomain.Extraction{
			Legibility: 0.88,
			Items: []ocrdomain.ExtractedItem{
				{Index: 0, RawText: "Amoxicilina 500mg", Product: ocrdomain.ExtractedProduct{Text: "Amoxicilina"}},
			},
		},
		InputTokens:  1200,
		OutputTokens: 300,
		CostUSD:      0.015,
	})

	matcher := &dummyMatcher{
		candidates: []ocrdomain.CandidateProduct{
			{CatalogID: uuid.New(), Name: "Amoxicilina 500mg", Score: 0.92},
		},
	}

	pipe := NewPipelineService(PipelineOptions{
		Preprocessor:     prep,
		Extractor:        extractor,
		Matcher:          matcher,
		Budget:           &dummyBudget{status: ocrdomain.BudgetStatus{MonthlyUSD: 300, CurrentUSD: 10}},
		Metrics:          &dummyMetrics{},
		Jobs:             jobRepo,
		PrescriptionRepo: rxRepo,
		UoW:              syncTxRunner{},
	})

	err := pipe.RunPipeline(context.Background(), ProcessDocumentInput{
		PrescriptionID: rxID,
		Attempt:        1,
		RawPages:       [][]byte{[]byte("raw-bytes")},
		Country:        "CO",
		PetSpecies:     "Canino",
	})
	if err != nil {
		t.Fatalf("expected pipeline to succeed, got: %v", err)
	}

	if jobRepo.lastStatus != "done" {
		t.Errorf("expected final status done, got %s", jobRepo.lastStatus)
	}
	if rx.Extraction == nil {
		t.Fatalf("extraction not recorded in aggregate")
	}
	if rx.Extraction.Legibility != 0.88 {
		t.Errorf("expected legibility 0.88, got %f", rx.Extraction.Legibility)
	}
	if len(rx.Extraction.Items[0].Product.Candidates) == 0 {
		t.Errorf("expected candidates to be attached by matcher")
	}
}

func TestPipeline_PoorQuality_Reupload(t *testing.T) {
	rxID := uuid.New()
	rx := setupTestPrescription(rxID, uuid.New(), uuid.New())
	rxRepo := &memoryRxRepo{rx: rx}
	jobRepo := &dummyJobRepo{}

	// QualityScore 0.20 < 0.35 threshold
	prep := &dummyPreprocessor{
		page: ocrdomain.PreprocessedPage{
			QualityScore: 0.20,
			IsTooBlurry:  true,
			Warnings:     []string{"Imagen borrosa"},
		},
	}

	calledExtractor := false
	pipe := NewPipelineService(PipelineOptions{
		Preprocessor: prep,
		Extractor: ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{
			Extraction: ocrdomain.Extraction{Legibility: 0.9},
		}),
		Jobs:             jobRepo,
		PrescriptionRepo: rxRepo,
		UoW:              syncTxRunner{},
	})

	err := pipe.RunPipeline(context.Background(), ProcessDocumentInput{
		PrescriptionID: rxID,
		Attempt:        1,
		RawPages:       [][]byte{[]byte("blurry")},
	})
	if err != nil {
		t.Fatalf("reupload request should not return raw error: %v", err)
	}

	if jobRepo.lastStatus != "reupload_requested" {
		t.Errorf("expected status reupload_requested, got %s", jobRepo.lastStatus)
	}
	if calledExtractor {
		t.Errorf("extractor should NOT be called when quality is poor (saving AI costs)")
	}
}

func TestPipeline_LowLegibility_Reupload(t *testing.T) {
	rxID := uuid.New()
	rx := setupTestPrescription(rxID, uuid.New(), uuid.New())
	rxRepo := &memoryRxRepo{rx: rx}
	jobRepo := &dummyJobRepo{}

	prep := &dummyPreprocessor{
		page: ocrdomain.PreprocessedPage{QualityScore: 0.80},
	}

	// Model returns legibility 0.25 < 0.40 threshold
	extractor := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{
		Extraction: ocrdomain.Extraction{
			Legibility: 0.25,
			Items:      []ocrdomain.ExtractedItem{},
		},
	})

	pipe := NewPipelineService(PipelineOptions{
		Preprocessor:     prep,
		Extractor:        extractor,
		Jobs:             jobRepo,
		PrescriptionRepo: rxRepo,
		UoW:              syncTxRunner{},
	})

	err := pipe.RunPipeline(context.Background(), ProcessDocumentInput{
		PrescriptionID: rxID,
		Attempt:        1,
		RawPages:       [][]byte{[]byte("legible-photo-unreadable-handwriting")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if jobRepo.lastStatus != "reupload_requested" {
		t.Errorf("expected status reupload_requested for low legibility, got %s", jobRepo.lastStatus)
	}
}

func TestPipeline_BudgetExceeded_HardStop(t *testing.T) {
	rxID := uuid.New()
	rx := setupTestPrescription(rxID, uuid.New(), uuid.New())
	rxRepo := &memoryRxRepo{rx: rx}
	jobRepo := &dummyJobRepo{}

	prep := &dummyPreprocessor{page: ocrdomain.PreprocessedPage{QualityScore: 0.80}}

	pipe := NewPipelineService(PipelineOptions{
		Preprocessor: prep,
		Budget: &dummyBudget{
			checkErr: ocrdomain.ErrBudgetExceeded,
		},
		Jobs:             jobRepo,
		PrescriptionRepo: rxRepo,
		UoW:              syncTxRunner{},
	})

	err := pipe.RunPipeline(context.Background(), ProcessDocumentInput{
		PrescriptionID: rxID,
		Attempt:        1,
		RawPages:       [][]byte{[]byte("photo")},
	})
	if !errors.Is(err, ocrdomain.ErrBudgetExceeded) {
		t.Errorf("expected ErrBudgetExceeded, got: %v", err)
	}
	if jobRepo.lastStatus != "failed" {
		t.Errorf("expected job failed, got %s", jobRepo.lastStatus)
	}
}

func TestPipeline_Refusal(t *testing.T) {
	rxID := uuid.New()
	rx := setupTestPrescription(rxID, uuid.New(), uuid.New())
	rxRepo := &memoryRxRepo{rx: rx}
	jobRepo := &dummyJobRepo{}

	prep := &dummyPreprocessor{page: ocrdomain.PreprocessedPage{QualityScore: 0.80}}
	extractor := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{
		Refused: true,
	})

	pipe := NewPipelineService(PipelineOptions{
		Preprocessor:     prep,
		Extractor:        extractor,
		Jobs:             jobRepo,
		PrescriptionRepo: rxRepo,
		UoW:              syncTxRunner{},
	})

	err := pipe.RunPipeline(context.Background(), ProcessDocumentInput{
		PrescriptionID: rxID,
		Attempt:        1,
		RawPages:       [][]byte{[]byte("photo")},
	})
	if !errors.Is(err, ocrdomain.ErrExtractionRefused) {
		t.Errorf("expected ErrExtractionRefused, got: %v", err)
	}
	if jobRepo.lastStatus != "refused" {
		t.Errorf("expected job status refused, got %s", jobRepo.lastStatus)
	}
}
