package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/webhooks"
)

func TestChecklist_AllScenariosPassAgainstMock(t *testing.T) {
	// Servidor simulador que responde conforme al contrato de Partner API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		// Verificación de audiencia y tokens (Escenario 1)
		if r.URL.Path == "/partner/v1/me" {
			if authHeader == "" || authHeader == "Bearer user-token-api-audience" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"organization_id":"00000000-0000-0000-0000-000000000001","display_name":"Farmacia Mock"}`))
			return
		}

		if r.URL.Path == "/api/v1/me" {
			if authHeader == "Bearer partner-token-partner-audience" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		// Escenario 2: Catalog listing
		if r.URL.Path == "/partner/v1/catalog/medications" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"00000000-0000-0000-0000-000000000001","name":"Amoxicilina 500mg"}]}`))
			return
		}

		// Escenario 3: Individual SKU mapping
		if r.Method == http.MethodPut && len(r.URL.Path) > len("/partner/v1/product-mappings/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sku":"SKU-SIM-001","active":true}`))
			return
		}

		// Escenario 4: Bulk SKU mappings
		if r.URL.Path == "/partner/v1/product-mappings/bulk" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"processed":2,"created":2,"updated":0}`))
			return
		}

		// Escenario 5: Quote requests inbox
		if r.URL.Path == "/partner/v1/quote-requests" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}

		// Escenarios 6 & 7: Quotes submit
		if r.Method == http.MethodPost && (len(r.URL.Path) > 10 && r.URL.Path[len(r.URL.Path)-7:] == "/quotes") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"quote_id":"00000000-0000-0000-0000-000000000002","status":"submitted"}`))
			return
		}

		// Escenario 8: Decline quote request
		if r.Method == http.MethodPost && (len(r.URL.Path) > 10 && r.URL.Path[len(r.URL.Path)-8:] == "/decline") {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Escenario 9: Orders status transition
		if r.Method == http.MethodPost && (len(r.URL.Path) > 10 && r.URL.Path[len(r.URL.Path)-7:] == "/status") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"confirmed"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	//nolint:gosec // mock test tokens
	runner := &ChecklistRunner{
		BaseURL:       mockServer.URL,
		PartnerToken:  "partner-token-partner-audience",
		UserToken:     "user-token-api-audience",
		WebhookSecret: "secret_primary_key_32_bytes_len!",
		HTTPClient:    mockServer.Client(),
	}

	results := runner.RunAll(context.Background())
	if len(results) != 10 {
		t.Fatalf("se esperaban 10 escenarios, se obtuvieron %d", len(results))
	}

	for _, res := range results {
		if !res.Passed {
			t.Errorf("Escenario %d (%s) falló: %s", res.Number, res.Name, res.Error)
		}
	}
}

func TestWebhook_HMACVerificationVectors(t *testing.T) {
	secret := []byte("secret_primary_key_32_bytes_len!")
	body := []byte(`{"id":"evt-123","event":"order.placed"}`)
	now := time.Now()
	ts := now.Unix()

	sig := webhooks.Sign(secret, ts, body)

	if err := webhooks.Verify(sig, [][]byte{secret}, ts, body, now, 300*time.Second); err != nil {
		t.Fatalf("firma válida fue rechazada: %v", err)
	}

	// Firma con cuerpo alterado
	tampered := []byte(`{"id":"evt-123","event":"order.placed","extra":1}`)
	if err := webhooks.Verify(sig, [][]byte{secret}, ts, tampered, now, 300*time.Second); err == nil {
		t.Fatalf("cuerpo alterado no debe ser válido")
	}

	// Firma con secreto incorrecto
	wrongSecret := []byte("wrong_secret_key_32_bytes_len!!!")
	if err := webhooks.Verify(sig, [][]byte{wrongSecret}, ts, body, now, 300*time.Second); err == nil {
		t.Fatalf("secreto incorrecto no debe ser válido")
	}
}
