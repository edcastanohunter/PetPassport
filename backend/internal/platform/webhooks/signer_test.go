package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWebhooks_SignAndVerify_Vectors(t *testing.T) {
	// Canonical test vectors according to ARQ-004 and SEC-003.
	secret := []byte("whsec_test_secret_32_bytes_long_12345")
	timestamp := int64(1731171600)
	body := []byte(`{"quote_request_id":"0193132e-4b67-7b2b-8a88-dcf46be1fa3a"}`)

	// Calcular vector esperado manualmente
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("1731171600."))
	mac.Write(body)
	expectedHex := hex.EncodeToString(mac.Sum(nil))
	expectedHeader := "v1=" + expectedHex

	gotSig := Sign(secret, timestamp, body)
	if gotSig != expectedHeader {
		t.Fatalf("Sign() = %s, want %s", gotSig, expectedHeader)
	}

	// Verificación exitosa en tiempo presente dentro de la ventana
	now := time.Unix(timestamp+60, 0) // 1 minuto después
	err := Verify(gotSig, [][]byte{secret}, timestamp, body, now, 300*time.Second)
	if err != nil {
		t.Fatalf("Verify() failed unexpectedly: %v", err)
	}
}

func TestWebhooks_Verify_ReplayProtection(t *testing.T) {
	secret := []byte("whsec_test_secret_32_bytes_long_12345")
	timestamp := int64(1731171600)
	body := []byte(`{"order_id":"0193132e-4b67-7b2b-8a88-dcf46be1fa3a"}`)
	sig := Sign(secret, timestamp, body)

	// Caso 1: Pasaron 301 segundos (> 300 s)
	nowPast := time.Unix(timestamp+301, 0)
	err := Verify(sig, [][]byte{secret}, timestamp, body, nowPast, 300*time.Second)
	if !errors.Is(err, ErrTimestampOutOfWindow) {
		t.Errorf("expected ErrTimestampOutOfWindow for expired timestamp, got: %v", err)
	}

	// Caso 2: Timestamp en el futuro > 300 s
	nowFuture := time.Unix(timestamp-301, 0)
	err = Verify(sig, [][]byte{secret}, timestamp, body, nowFuture, 300*time.Second)
	if !errors.Is(err, ErrTimestampOutOfWindow) {
		t.Errorf("expected ErrTimestampOutOfWindow for future timestamp, got: %v", err)
	}
}

func TestWebhooks_Verify_RotationDoubleSecret(t *testing.T) {
	currentSecret := []byte("secret_current_32_bytes_alpha_123")
	previousSecret := []byte("secret_previous_32_bytes_beta_456")
	otherSecret := []byte("secret_other_32_bytes_gamma_789")

	timestamp := time.Now().Unix()
	body := []byte(`{"ping":"true"}`)

	// Firma generada con el secreto previo (todavía en transición de rotación)
	sigPrevious := Sign(previousSecret, timestamp, body)

	// Ambos secretos activos durante la rotación (ARQ-004)
	activeSecrets := [][]byte{currentSecret, previousSecret}

	err := Verify(sigPrevious, activeSecrets, timestamp, body, time.Now(), 300*time.Second)
	if err != nil {
		t.Fatalf("Verify() should accept previous secret during rotation: %v", err)
	}

	// Firma doble combinada en la cabecera
	sigCombined := SignMultiple(activeSecrets, timestamp, body)
	if !strings.Contains(sigCombined, ",") {
		t.Fatalf("expected combined header to contain comma, got %s", sigCombined)
	}

	// Verificar contra un cliente que solo tiene el secreto actual
	err = Verify(sigCombined, [][]byte{currentSecret}, timestamp, body, time.Now(), 300*time.Second)
	if err != nil {
		t.Fatalf("Verify() should accept current secret from combined header: %v", err)
	}

	// Secreto ajeno falla
	err = Verify(sigCombined, [][]byte{otherSecret}, timestamp, body, time.Now(), 300*time.Second)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature for unauthorized secret, got: %v", err)
	}
}

func TestWebhooks_Verify_TamperedBody(t *testing.T) {
	secret := []byte("whsec_test_secret_32_bytes_long_12345")
	timestamp := time.Now().Unix()
	originalBody := []byte(`{"amount":100}`)
	tamperedBody := []byte(`{"amount":999}`)

	sig := Sign(secret, timestamp, originalBody)

	err := Verify(sig, [][]byte{secret}, timestamp, tamperedBody, time.Now(), 300*time.Second)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature for tampered body, got: %v", err)
	}
}
