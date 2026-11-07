package mock

import (
	"context"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// Extractor simula respuestas pregrabadas para pruebas y evaluación CI sin coste (PP-F4-QA-003).
type Extractor struct {
	Responses   map[string]ocrdomain.ExtractionResult
	DefaultResp ocrdomain.ExtractionResult
	CustomID    string
}

func NewMockExtractor(defaultResp ocrdomain.ExtractionResult) *Extractor {
	return &Extractor{
		Responses:   make(map[string]ocrdomain.ExtractionResult),
		DefaultResp: defaultResp,
		CustomID:    "mock:test-v1",
	}
}

func (m *Extractor) ID() string {
	if m.CustomID != "" {
		return m.CustomID
	}
	return "mock:test-v1"
}

func (m *Extractor) Extract(ctx context.Context, pages []ocrdomain.PreprocessedPage, hints ocrdomain.ExtractorHints) (ocrdomain.ExtractionResult, error) {
	if len(pages) == 0 {
		return ocrdomain.ExtractionResult{}, ocrdomain.ErrInvalidDocument
	}

	if resp, ok := m.Responses[string(pages[0].Sha256)]; ok {
		if resp.Refused {
			return resp, ocrdomain.ErrExtractionRefused
		}
		return resp, nil
	}

	if m.DefaultResp.Refused {
		return m.DefaultResp, ocrdomain.ErrExtractionRefused
	}
	return m.DefaultResp, nil
}

var _ ocrdomain.DocumentExtractor = (*Extractor)(nil)
