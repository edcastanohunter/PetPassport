package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

func TestClaudeExtractor_Success(t *testing.T) {
	expectedExtraction := ocrdomain.Extraction{
		Legibility: 0.95,
		Warnings:   []string{},
		Header: ocrdomain.ExtractedHeader{
			VetName: ptr("Dr. Laura Mora"),
			Clinic:  ptr("Veterinaria La Colina"),
			PetName: ptr("Max"),
			Species: ptr("Canino"),
		},
		Items: []ocrdomain.ExtractedItem{
			{
				Index:   0,
				RawText: "Amoxicilina 250mg c/12h x 7 días",
				Product: ocrdomain.ExtractedProduct{Text: "Amoxicilina 250mg"},
				Dose:    "250mg",
			},
		},
	}
	extractionBytes, _ := json.Marshal(expectedExtraction)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Respuesta compatible con messages.New
		resp := map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": string(extractionBytes),
				},
			},
			"model":       "claude-opus-5-5",
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  1500,
				"output_tokens": 500,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Creamos un transport que redirige a nuestro test server
	customTransport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = srv.Listener.Addr().String()
		srv.Config.Handler.ServeHTTP(w, r)
	})
	testClient := &http.Client{Transport: roundTripperFunc(customTransport)}

	ext, err := NewExtractor(Config{
		APIKey:       "test-api-key",
		Model:        "claude-opus-5-5",
		HTTPClient:   testClient,
		PriceInMTok:  4.0,
		PriceOutMTok: 20.0,
	})
	if err != nil {
		t.Fatalf("new extractor failed: %v", err)
	}

	pages := []ocrdomain.PreprocessedPage{
		{
			PageNumber:   1,
			Data:         []byte("fake-image-bytes"),
			ContentType:  "image/jpeg",
			QualityScore: 0.95,
		},
	}

	res, err := ext.Extract(context.Background(), pages, ocrdomain.ExtractorHints{PetSpecies: "Canino"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if res.Extraction.Header.VetName == nil || *res.Extraction.Header.VetName != "Dr. Laura Mora" {
		t.Errorf("expected vet name Dr. Laura Mora, got %v", res.Extraction.Header.VetName)
	}
	if len(res.Extraction.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Extraction.Items))
	}
	// Comprobar cálculo de coste: (1500 * 4 + 500 * 20) / 1_000_000 = (6000 + 10000)/1_000_000 = 0.016 USD
	expectedCost := (1500.0*4.0 + 500.0*20.0) / 1_000_000.0
	if res.CostUSD != expectedCost {
		t.Errorf("expected cost %f, got %f", expectedCost, res.CostUSD)
	}
}

func TestClaudeExtractor_Refusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":          "msg_refusal",
			"type":        "message",
			"role":        "assistant",
			"content":     []any{},
			"model":       "claude-opus-5-5",
			"stop_reason": "refusal",
			"usage": map[string]any{
				"input_tokens":  200,
				"output_tokens": 10,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	customTransport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = srv.Listener.Addr().String()
		srv.Config.Handler.ServeHTTP(w, r)
	})
	testClient := &http.Client{Transport: roundTripperFunc(customTransport)}

	ext, err := NewExtractor(Config{
		APIKey:     "test-key",
		HTTPClient: testClient,
	})
	if err != nil {
		t.Fatalf("new extractor failed: %v", err)
	}

	pages := []ocrdomain.PreprocessedPage{
		{PageNumber: 1, Data: []byte("img")},
	}
	res, err := ext.Extract(context.Background(), pages, ocrdomain.ExtractorHints{})
	if err == nil {
		t.Fatalf("expected error on refusal")
	}
	if !res.Refused {
		t.Errorf("expected Refused=true")
	}
}

func ptr[T any](v T) *T { return &v }

type roundTripperFunc func(w http.ResponseWriter, r *http.Request)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f(rec, req)
	return rec.Result(), nil
}
