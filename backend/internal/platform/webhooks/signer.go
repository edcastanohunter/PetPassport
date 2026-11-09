// Package webhooks implementa la firma criptográfica HMAC-SHA256, verificación anti-replay
// y despacho seguro de webhooks salientes hacia partners (PP-F4-ARQ-004, PTN-004, SEC-003).
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// DefaultMaxAge define la ventana máxima de validez del timestamp (300 s / 5 minutos) (ARQ-004, SEC-003).
	DefaultMaxAge = 300 * time.Second

	// SignaturePrefix es el prefijo de versión de la firma.
	SignaturePrefix = "v1="
)

var (
	ErrTimestampOutOfWindow = errors.New("webhooks: timestamp fuera de la ventana de tolerancia de 300 s")
	ErrInvalidSignature     = errors.New("webhooks: firma HMAC inválida")
	ErrMissingSignature     = errors.New("webhooks: cabecera de firma vacía o con formato no soportado")
	ErrNoSecrets            = errors.New("webhooks: no hay secretos configurados para validar la firma")
)

// Sign calcula la firma HMAC-SHA256 sobre timestamp.body para un único secreto.
// Formato resultante: "v1=<hex HMAC-SHA256(secret, timestamp + "." + body)>"
func Sign(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	mac.Write(body)
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// SignMultiple calcula las firmas para múltiples secretos activos (soporte para rotación con doble secreto).
// Formato resultante: "v1=<sig1>,v1=<sig2>"
func SignMultiple(secrets [][]byte, timestamp int64, body []byte) string {
	if len(secrets) == 0 {
		return ""
	}
	sigs := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		if len(sec) > 0 {
			sigs = append(sigs, Sign(sec, timestamp, body))
		}
	}
	return strings.Join(sigs, ",")
}

// Verify verifica que la cabecera X-PP-Signature contenga al menos una firma válida para
// los secretos activos y que el timestamp esté dentro de la ventana de tolerancia (anti-replay).
func Verify(signatureHeader string, secrets [][]byte, timestamp int64, body []byte, now time.Time, maxAge time.Duration) error {
	if len(secrets) == 0 {
		return ErrNoSecrets
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}

	// 1. Comprobación de ventana de tiempo (anti-replay)
	diff := time.Duration(math.Abs(float64(now.Unix()-timestamp))) * time.Second
	if diff > maxAge {
		return fmt.Errorf("%w: diferencia de %v excede límite de %v", ErrTimestampOutOfWindow, diff, maxAge)
	}

	if signatureHeader == "" {
		return ErrMissingSignature
	}

	// 2. Extraer las firmas v1 presentes en la cabecera separadas por coma
	parts := strings.Split(signatureHeader, ",")
	candidateHexes := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, SignaturePrefix) {
			candidateHexes = append(candidateHexes, strings.TrimPrefix(p, SignaturePrefix))
		}
	}
	if len(candidateHexes) == 0 {
		return ErrMissingSignature
	}

	// 3. Comparación en tiempo constante contra cada secreto activo
	for _, secret := range secrets {
		if len(secret) == 0 {
			continue
		}
		expectedSig := Sign(secret, timestamp, body)
		expectedHex := strings.TrimPrefix(expectedSig, SignaturePrefix)
		expectedBytes, err := hex.DecodeString(expectedHex)
		if err != nil {
			continue
		}

		for _, cand := range candidateHexes {
			candBytes, err := hex.DecodeString(cand)
			if err != nil {
				continue
			}
			if hmac.Equal(expectedBytes, candBytes) {
				return nil
			}
		}
	}

	return ErrInvalidSignature
}
