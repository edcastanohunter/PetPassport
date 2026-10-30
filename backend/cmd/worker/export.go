package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Exportación de los datos de un titular (PP-F2-SEC-006, ADR-023): JSON con la
// cuenta, las mascotas, el timeline visible al tutor, el carnet, las citas y
// las entregas de notificación. Es una operación manual del equipo de soporte
// ante una solicitud verificada del titular; el runbook (§7.16) describe la
// verificación de identidad y la entrega segura del archivo. No incluye
// secretos, hashes ni claves de objetos internas.

// exportFormat versiona el contrato del archivo.
const exportFormat = "petpassport-export/1"

// rowQuerier es lo que la exportación necesita de la transacción.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// exportSection es una sección del archivo: una consulta que devuelve un único
// valor JSON (objeto o lista). $1 es el id del usuario.
type exportSection struct {
	key string
	sql string
	// required: si no hay fila (la cuenta no existe) la exportación falla.
	required bool
}

const petIDs = `SELECT id FROM read_petregistry.pets WHERE guardian_id = $1`

var exportSections = []exportSection{
	{"account", `SELECT to_jsonb(u) FROM identity.user_accounts u WHERE u.id = $1`, true},
	{"pets", `SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.created_at, p.id), '[]'::jsonb)
		FROM read_petregistry.pets p WHERE p.guardian_id = $1`, false},
	{"care_links", `SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY l.granted_at, l.pet_id), '[]'::jsonb)
		FROM read_petregistry.pet_care_links l WHERE l.pet_id IN (` + petIDs + `)`, false},
	{"care_link_requests", `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.created_at, r.id), '[]'::jsonb)
		FROM read_petregistry.care_link_requests r WHERE r.pet_id IN (` + petIDs + `)`, false},
	// Solo lo visible para el tutor: lo reservado a la organización no se exporta.
	{"timeline", `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY t.occurred_at, t.position), '[]'::jsonb)
		FROM read_clinical.medical_timeline t
		WHERE t.visibility IN ('guardian', 'both') AND t.pet_id IN (` + petIDs + `)`, false},
	{"vaccination_card", `SELECT coalesce(jsonb_agg((to_jsonb(c) - 'evidence_object_key') ORDER BY c.administered_at, c.id), '[]'::jsonb)
		FROM read_clinical.card_entries c WHERE c.pet_id IN (` + petIDs + `)`, false},
	{"appointments", `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.created_at, a.id), '[]'::jsonb)
		FROM read_scheduling.appointments a WHERE a.guardian_id = $1`, false},
	{"notification_preferences", `SELECT to_jsonb(p) - 'unsubscribe_token_hash'
		FROM notifications.preferences p WHERE p.user_id = $1`, false},
	// Sin payload ni hash de la dirección: plantilla, estado y fechas.
	{"notification_deliveries", `SELECT coalesce(jsonb_agg(jsonb_build_object(
			'template', d.template, 'category', d.category, 'status', d.status,
			'created_at', d.created_at, 'sent_at', d.sent_at) ORDER BY d.created_at, d.id), '[]'::jsonb)
		FROM notifications.deliveries d WHERE d.recipient_id = $1`, false},
}

// buildExport ejecuta las secciones sobre q y compone el archivo.
func buildExport(ctx context.Context, q rowQuerier, userID uuid.UUID, now time.Time) (map[string]any, error) {
	out := map[string]any{
		"format":      exportFormat,
		"user_id":     userID.String(),
		"exported_at": now.UTC().Format(time.RFC3339),
	}
	for _, s := range exportSections {
		var raw []byte
		err := q.QueryRow(ctx, s.sql, userID).Scan(&raw)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if s.required {
				return nil, fmt.Errorf("export: el usuario %s no existe", userID)
			}
			out[s.key] = nil
			continue
		case err != nil:
			return nil, fmt.Errorf("export: sección %s: %w", s.key, err)
		}
		out[s.key] = json.RawMessage(raw)
	}
	return out, nil
}

// exportUser implementa `worker export --user <id> [--out archivo]`. Lee en una
// transacción de solo lectura para que las secciones sean coherentes entre sí.
func exportUser(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	user := fs.String("user", "", "id (UUID) del titular a exportar")
	outPath := fs.String("out", "", "archivo de salida (por defecto, stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := uuid.Parse(*user)
	if err != nil {
		return errors.New("export: --user <uuid> es obligatorio")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, err := open(ctx, "petpassport-worker-cli")
	if err != nil {
		return err
	}
	defer a.close()

	var doc map[string]any
	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if doc, err = buildExport(ctx, tx, id, time.Now()); err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	// Solo el id (no es PII): deja rastro de que se generó el archivo.
	a.tel.Logger.Info("export generated", "user_id", id.String())
	return nil
}
