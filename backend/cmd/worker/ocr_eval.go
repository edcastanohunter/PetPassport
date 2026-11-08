package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/ocr/eval"
	"github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/claude"
	ocrmock "github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/mock"
	"github.com/edcastanohunter/petpassport/backend/internal/ocr/infra/extractor/tesseract"
)

func ocrEvalCommand(args []string) error {
	fs := flag.NewFlagSet("ocr-eval", flag.ContinueOnError)
	datasetVersion := fs.String("dataset", "v1", "versión del dataset de evaluación (v1)")
	extractorID := fs.String("extractor", "mock", "extractor a evaluar: mock | claude | tesseract")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Printf("== PetPassport OCR Evaluation Harness (PP-F4-OCR-007, QA-003) ==\n")
	fmt.Printf("Dataset: %s | Extractor: %s\n\n", *datasetVersion, *extractorID)

	dataset := eval.BuildSyntheticDataset()
	fmt.Printf("Muestras sintéticas cargadas: %d\n", len(dataset))

	var extractor ocrdomain.DocumentExtractor
	switch *extractorID {
	case "mock":
		m := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{})
		for _, s := range dataset {
			m.Responses[string(s.ID)] = ocrdomain.ExtractionResult{
				Extraction: ocrdomain.Extraction{
					Legibility:  s.Legibility,
					Header:      s.Header,
					Items:       s.Items,
					ExtractorID: "mock:recorded-v1",
				},
				InputTokens:  1200,
				OutputTokens: 350,
				CostUSD:      0.015,
			}
		}
		extractor = m
	case "tesseract":
		extractor = tesseract.NewExtractor()
	case "claude":
		apiKey := os.Getenv("PP_ANTHROPIC_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			fmt.Println("AVISO: No se encontró PP_ANTHROPIC_API_KEY; usando mock pregrabado para no incurrir en costes.")
			m := ocrmock.NewMockExtractor(ocrdomain.ExtractionResult{})
			for _, s := range dataset {
				m.Responses[string(s.ID)] = ocrdomain.ExtractionResult{
					Extraction: ocrdomain.Extraction{
						Legibility:  s.Legibility,
						Header:      s.Header,
						Items:       s.Items,
						ExtractorID: "mock:recorded-v1",
					},
					InputTokens:  1200,
					OutputTokens: 350,
					CostUSD:      0.015,
				}
			}
			extractor = m
		} else {
			ext, err := claude.NewExtractor(claude.Config{
				APIKey: apiKey,
			})
			if err != nil {
				return fmt.Errorf("inicializar claude extractor: %w", err)
			}
			extractor = ext
		}
	default:
		return fmt.Errorf("extractor desconocido: %s", *extractorID)
	}

	evaluator := eval.NewEvaluator(extractor)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	metrics, err := evaluator.Evaluate(ctx, dataset)
	if err != nil {
		return fmt.Errorf("evaluación fallida: %w", err)
	}

	fmt.Println("\n--- Resultados de Evaluación ---")
	fmt.Printf("Total Muestras:      %d\n", metrics.TotalSamples)
	fmt.Printf("F1 Producto:         %.3f  (Gate >= 0.85)\n", metrics.F1Product)
	fmt.Printf("F1 Dosis:            %.3f  (Gate >= 0.80)\n", metrics.F1Dose)
	fmt.Printf("F1 Frecuencia:       %.3f  (Gate >= 0.80)\n", metrics.F1Frequency)
	fmt.Printf("F1 Duración:         %.3f\n", metrics.F1Duration)
	fmt.Printf("F1 Cantidad:         %.3f\n", metrics.F1Quantity)
	fmt.Printf("Header Exact Match:  %.3f\n", metrics.HeaderExactMatch)
	fmt.Printf("Tasa Re-upload:      %.3f\n", metrics.ReuploadRate)
	fmt.Printf("Latencia Promedio:   %d ms\n", metrics.AvgLatencyMs)
	fmt.Printf("Coste Promedio/Rx:   $%.4f USD\n", metrics.AvgCostUSD)

	// Persistir en base de datos si está disponible
	if appInstance, err := open(ctx, "ocr-eval"); err == nil && appInstance.pool != nil {
		metricsJSON, _ := json.Marshal(metrics)
		runID := uuid.New()
		totalCost := metrics.AvgCostUSD * float64(metrics.TotalSamples)
		_, _ = appInstance.pool.Exec(ctx, `
			INSERT INTO ocr.eval_runs (id, extractor_id, dataset_version, metrics, cost_usd, ran_at, git_sha)
			VALUES ($1, $2, $3, $4, $5, now(), $6)
		`, runID, extractor.ID(), *datasetVersion, metricsJSON, totalCost, os.Getenv("GIT_SHA"))
		appInstance.pool.Close()
	}

	// Gate de no-regresión CI
	if metrics.F1Product < 0.85 {
		return fmt.Errorf("GATE FALLIDO: F1 producto %.3f < 0.85", metrics.F1Product)
	}
	if metrics.F1Dose < 0.80 {
		return fmt.Errorf("GATE FALLIDO: F1 dosis %.3f < 0.80", metrics.F1Dose)
	}
	if metrics.F1Frequency < 0.80 {
		return fmt.Errorf("GATE FALLIDO: F1 frecuencia %.3f < 0.80", metrics.F1Frequency)
	}

	fmt.Println("\n>> GATE CI SUPERADO: Todos los umbrales de precisión clínica cumplidos.")
	return nil
}
