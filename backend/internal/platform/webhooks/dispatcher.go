package webhooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/egress"
)

// RetryDelays define los intervalos de reintento según ARQ-004: 1m, 5m, 30m, 2h, 12h (5 intentos).
var RetryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	12 * time.Hour,
}

// MaxAttempts es el número máximo de intentos antes de marcar el envío como failed (DLQ).
const MaxAttempts = 5

// DeliveryTarget contiene la configuración de destino de un partner para webhooks.
type DeliveryTarget struct {
	OrganizationID uuid.UUID
	URL            string
	Secrets        [][]byte // Secreto actual y secreto previo si está en rotación
}

// Dispatcher gestiona la persistencia, firma y envío de webhooks salientes (ARQ-004).
type Dispatcher struct {
	pool   *pgxpool.Pool
	client *egress.Client
	now    func() time.Time
}

// NewDispatcher construye un despachador de webhooks.
func NewDispatcher(pool *pgxpool.Pool, client *egress.Client) *Dispatcher {
	if client == nil {
		client = egress.NewWebhookClient(egress.Config{
			AllowedHosts: []string{"*"},
		})
	}
	return &Dispatcher{
		pool:   pool,
		client: client,
		now:    time.Now,
	}
}

// DeliveryResult resume el resultado de un intento de entrega.
type DeliveryResult struct {
	DeliveryID uuid.UUID
	StatusCode int
	Success    bool
	Error      string
}

// Deliver envía de forma inmediata o reintenta un webhook saliente hacia el partner.
func (d *Dispatcher) Deliver(ctx context.Context, target DeliveryTarget, event string, payload any) (DeliveryResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("webhooks: serializar payload: %w", err)
	}

	payloadHash := sha256.Sum256(body)
	deliveryID := uuid.Must(uuid.NewV7())
	recordID := uuid.New()
	now := d.now()
	timestamp := now.Unix()

	// Firma HMAC con los secretos disponibles
	signature := SignMultiple(target.Secrets, timestamp, body)

	// Persistir estado inicial en webhook_deliveries si hay pool
	if d.pool != nil {
		_, _ = d.pool.Exec(ctx, `
			INSERT INTO read_partnernetwork.webhook_deliveries (
				id, organization_id, event, delivery_id, payload_hash, status, attempts, next_attempt_at, created_at
			) VALUES ($1, $2, $3, $4, $5, 'queued', 0, $6, $7)
			ON CONFLICT (delivery_id) DO NOTHING`,
			recordID, target.OrganizationID, event, deliveryID, payloadHash[:], now, now,
		)
	}

	res := d.executeHTTP(ctx, target, event, deliveryID, timestamp, signature, body)

	// Actualizar registro en base de datos
	if d.pool != nil {
		status := "queued"
		var nextAttempt *time.Time
		var sentAt *time.Time

		if res.Success {
			status = "sent"
			nowSent := d.now()
			sentAt = &nowSent
		} else {
			// Calcular siguiente reintento
			if len(RetryDelays) > 0 {
				next := now.Add(RetryDelays[0])
				nextAttempt = &next
			}
		}

		_, _ = d.pool.Exec(ctx, `
			UPDATE read_partnernetwork.webhook_deliveries
			SET status = $1, attempts = attempts + 1, last_status_code = $2, last_error = $3,
			    next_attempt_at = $4, sent_at = $5
			WHERE delivery_id = $6`,
			status, res.StatusCode, res.Error, nextAttempt, sentAt, deliveryID,
		)
	}

	return res, nil
}

// RetryDelivery procesa un reintento para una entrega existente.
func (d *Dispatcher) RetryDelivery(ctx context.Context, deliveryID uuid.UUID, target DeliveryTarget, payload []byte) (DeliveryResult, error) {
	if d.pool == nil {
		return DeliveryResult{}, errors.New("webhooks: pool no disponible para reintento")
	}

	var attempts int
	var currentStatus, event string
	err := d.pool.QueryRow(ctx, `
		SELECT attempts, status, event
		FROM read_partnernetwork.webhook_deliveries
		WHERE delivery_id = $1`, deliveryID).Scan(&attempts, &currentStatus, &event)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("consultar entrega: %w", err)
	}

	now := d.now()
	timestamp := now.Unix()
	signature := SignMultiple(target.Secrets, timestamp, payload)

	res := d.executeHTTP(ctx, target, event, deliveryID, timestamp, signature, payload)

	newAttempts := attempts + 1
	newStatus := "queued"
	var nextAttempt *time.Time
	var sentAt *time.Time

	if res.Success {
		newStatus = "sent"
		nowSent := d.now()
		sentAt = &nowSent
	} else if newAttempts >= MaxAttempts {
		newStatus = "failed"
	} else {
		delayIndex := newAttempts - 1
		if delayIndex >= len(RetryDelays) {
			delayIndex = len(RetryDelays) - 1
		}
		next := now.Add(RetryDelays[delayIndex])
		nextAttempt = &next
	}

	_, err = d.pool.Exec(ctx, `
		UPDATE read_partnernetwork.webhook_deliveries
		SET status = $1, attempts = $2, last_status_code = $3, last_error = $4,
		    next_attempt_at = $5, sent_at = $6
		WHERE delivery_id = $7`,
		newStatus, newAttempts, res.StatusCode, res.Error, nextAttempt, sentAt, deliveryID,
	)

	return res, err
}

func (d *Dispatcher) executeHTTP(ctx context.Context, target DeliveryTarget, event string, deliveryID uuid.UUID, timestamp int64, signature string, body []byte) DeliveryResult {
	result := DeliveryResult{
		DeliveryID: deliveryID,
	}

	// Comprobar anti-SSRF y allowlist
	if err := d.client.CheckAllowed(target.URL); err != nil {
		result.Error = err.Error()
		return result
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		result.Error = fmt.Errorf("construir request: %w", err).Error()
		return result
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PP-Event", event)
	req.Header.Set("X-PP-Delivery-ID", deliveryID.String())
	req.Header.Set("X-PP-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-PP-Signature", signature)

	resp, err := d.client.HTTPClient().Do(req)
	if err != nil {
		result.Error = err.Error()
		d.client.RecordFailure(req.URL.Hostname())
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	result.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
		d.client.RecordSuccess(req.URL.Hostname())
	} else {
		d.client.RecordFailure(req.URL.Hostname())
		// Leer primeros 200 bytes para diagnóstico (ARQ-004)
		lr := io.LimitReader(resp.Body, 200)
		bodySnippet, _ := io.ReadAll(lr)
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodySnippet))
	}

	return result
}
