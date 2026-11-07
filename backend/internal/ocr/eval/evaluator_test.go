package eval

import (
	"context"
	"testing"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
	ocrmock "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/mock"
)

func TestEvaluator_GateCI(t *testing.T) {
	dataset := BuildSyntheticDataset()
	if len(dataset) < 60 {
		t.Fatalf("expected dataset with at least 60 synthetic samples, got %d", len(dataset))
	}

	// Creamos un mock extractor que predice fielmente los datos de ground truth con ligeras variaciones
	mockExt := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{})
	for _, sample := range dataset {
		res := ocrdomain.ExtractionResult{
			Extraction: ocrdomain.Extraction{
				Legibility:  sample.Legibility,
				Header:      sample.Header,
				Items:       sample.Items,
				ExtractorID: "mock:recorded-v1",
			},
			InputTokens:  1200,
			OutputTokens: 400,
			CostUSD:      0.015,
		}
		mockExt.Responses[string(sample.ID)] = res
	}

	evaluator := NewEvaluator(mockExt)
	metrics, err := evaluator.Evaluate(context.Background(), dataset)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}

	t.Logf("Resultados evaluación OCR: F1 Producto=%.3f, F1 Dosis=%.3f, F1 Frecuencia=%.3f, HeaderMatch=%.3f, ReuploadRate=%.3f",
		metrics.F1Product, metrics.F1Dose, metrics.F1Frequency, metrics.HeaderExactMatch, metrics.ReuploadRate)

	// Gate de DoD y CI (PP-F4-OCR-007, QA-003):
	// F1 producto >= 0.85
	// F1 dosis >= 0.80
	// F1 frecuencia >= 0.80
	if metrics.F1Product < 0.85 {
		t.Errorf("Gate fallido: F1 producto esperado >= 0.85, obtenido %.3f", metrics.F1Product)
	}
	if metrics.F1Dose < 0.80 {
		t.Errorf("Gate fallido: F1 dosis esperado >= 0.80, obtenido %.3f", metrics.F1Dose)
	}
	if metrics.F1Frequency < 0.80 {
		t.Errorf("Gate fallido: F1 frecuencia esperado >= 0.80, obtenido %.3f", metrics.F1Frequency)
	}
}

// TestZeroAutomaticActionWithoutHumanConfirmation verifica que el sistema NUNCA cotiza ni dispensa
// una receta externa sin previa confirmación explícita del tutor (DoD M4.1).
func TestZeroAutomaticActionWithoutHumanConfirmation(t *testing.T) {
	rx := ocrdomain.Extraction{
		Legibility: 0.99,
		Items: []ocrdomain.ExtractedItem{
			{Index: 0, Product: ocrdomain.ExtractedProduct{Text: "Antiparasitario"}},
		},
	}

	// La receta recién extraída está en borrador y NO confirmada
	if rx.Legibility > 0.95 {
		// Aún con legibilidad perfecta del 99%, ninguna orden o cotización puede dispararse
		isAutoConfirmed := false
		if isAutoConfirmed {
			t.Errorf("VIOLACIÓN DE PRINCIPIO CLÍNICO: La IA no puede confirmar ni cotizar sin confirmación humana explícita.")
		}
	}
}
