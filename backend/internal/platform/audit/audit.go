// Package audit escribe en platform.audit_log (docs/phase-1/06 §7, 08 §8).
// Para acciones no event-sourced (identity, admin) y decisiones relevantes.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Entry es una entrada de auditoría. Details nunca contiene PII en claro.
type Entry struct {
	ActorID       *uuid.UUID
	ActorRoles    []string
	Action        string
	TargetType    string
	TargetID      *uuid.UUID
	Reason        string
	Details       map[string]any
	CorrelationID *uuid.UUID
	IPHash        []byte
}

// Record inserta la entrada dentro de la transacción dada.
func Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	var details []byte
	if e.Details != nil {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("audit details: %w", err)
		}
		details = b
	}
	roles := e.ActorRoles
	if roles == nil {
		roles = []string{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.audit_log (id, actor_id, actor_roles, action, target_type, target_id, reason, details, correlation_id, ip_hash)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, NULLIF($7, ''), $8, $9, $10)`,
		uuid.Must(uuid.NewV7()), e.ActorID, roles, e.Action, e.TargetType, e.TargetID, e.Reason, details, e.CorrelationID, e.IPHash)
	if err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	return nil
}

// HashIP devuelve SHA-256(sal | día | ip) para correlacionar sin guardar la IP en claro.
func HashIP(salt, remoteAddr string, now time.Time) []byte {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	sum := sha256.Sum256([]byte(salt + "|" + now.UTC().Format("2006-01-02") + "|" + host))
	return sum[:]
}
