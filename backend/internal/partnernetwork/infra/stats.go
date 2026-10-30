package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// StatsProjector mantiene read_partnernetwork.org_stats_monthly (PRD-026).
// Lee el JSON crudo de los eventos de otros contextos (Partner Network no
// importa sus dominios, ADR-001) y atribuye cada evento al mes UTC de su
// ocurrencia. Idempotente por event_id: reprocesar un evento no duplica.
type StatsProjector struct{}

var _ projection.AsyncProjector = StatsProjector{}

// NewStatsProjector construye el proyector.
func NewStatsProjector() StatsProjector { return StatsProjector{} }

// Name implementa projection.Projector.
func (StatsProjector) Name() string { return app.ProjectionOrgStats }

// statByEvent asocia cada tipo de evento con su contador.
var statByEvent = map[string]string{
	"EncounterSigned":          app.StatEncountersSigned,
	"AppointmentConfirmed":     app.StatApptConfirmed,
	"AppointmentCompleted":     app.StatApptCompleted,
	"AppointmentCancelled":     app.StatApptCancelled,
	"AppointmentNoShow":        app.StatApptNoShow,
	"CareLinkGranted":          app.StatNewPatients,
	"VaccinationEntryRecorded": app.StatVaccinations,
}

// Handles implementa projection.Projector.
func (StatsProjector) Handles() []string {
	out := make([]string, 0, len(statByEvent))
	for t := range statByEvent {
		out = append(out, t)
	}
	return out
}

// Project implementa projection.Projector.
func (StatsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var p struct {
		OrganizationID string `json:"organization_id"`
		RecordedBy     *struct {
			OrganizationID string `json:"organization_id"`
		} `json:"recorded_by"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("org_stats: %s: %w", e.Type, err)
	}
	org := p.OrganizationID
	if org == "" && p.RecordedBy != nil {
		org = p.RecordedBy.OrganizationID
	}
	if org == "" {
		return nil // evento sin organización: no cuenta
	}
	tag, err := tx.Exec(ctx, `INSERT INTO read_partnernetwork.org_stats_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, e.EventID)
	if err != nil {
		return fmt.Errorf("org_stats: dedupe: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	at := e.OccurredAt.UTC()
	month := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	key := statByEvent[e.Type]
	_, err = tx.Exec(ctx, `
		INSERT INTO read_partnernetwork.org_stats_monthly (organization_id, month, metrics, updated_at)
		VALUES ($1, $2, jsonb_build_object($3::text, 1), $4)
		ON CONFLICT (organization_id, month) DO UPDATE SET
		  metrics = jsonb_set(read_partnernetwork.org_stats_monthly.metrics, ARRAY[$3::text],
		            to_jsonb(COALESCE((read_partnernetwork.org_stats_monthly.metrics->>$3)::int, 0) + 1)),
		  updated_at = EXCLUDED.updated_at`,
		org, month, key, at)
	if err != nil {
		return fmt.Errorf("org_stats: contar %s: %w", key, err)
	}
	return nil
}

// Reset implementa projection.AsyncProjector.
func (StatsProjector) Reset(ctx context.Context, tx pgx.Tx) error {
	for _, t := range []string{"org_stats_events", "org_stats_monthly"} {
		if _, err := tx.Exec(ctx, `DELETE FROM read_partnernetwork.`+t); err != nil {
			return fmt.Errorf("org_stats: reset: %w", err)
		}
	}
	return nil
}

// StatsQueries implementa app.StatsQueries.
type StatsQueries struct{ pool *pgxpool.Pool }

var _ app.StatsQueries = (*StatsQueries)(nil)

// NewStatsQueries construye las consultas.
func NewStatsQueries(pool *pgxpool.Pool) *StatsQueries { return &StatsQueries{pool: pool} }

// PreviousMonth devuelve el mes anterior a month (YYYY-MM).
func PreviousMonth(month string) (string, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, -1, 0).Format("2006-01"), nil
}

// OrgStats implementa el puerto. open_follow_ups se calcula al consultar
// (controles firmados con fecha de vencimiento de hoy en adelante) sobre el
// read model de Clinical: es un conteo puntual, no un contador mensual.
func (q *StatsQueries) OrgStats(ctx context.Context, orgID uuid.UUID, month string) (app.OrgStatsView, error) {
	prev, err := PreviousMonth(month)
	if err != nil {
		return app.OrgStatsView{}, err
	}
	var out app.OrgStatsView
	if out.Current, err = q.month(ctx, orgID, month); err != nil {
		return app.OrgStatsView{}, err
	}
	if out.Previous, err = q.month(ctx, orgID, prev); err != nil {
		return app.OrgStatsView{}, err
	}
	if err := q.pool.QueryRow(ctx, `
		SELECT count(*) FROM read_clinical.encounters
		 WHERE organization_id = $1 AND status = 'signed' AND follow_up IS NOT NULL
		   AND (follow_up->>'due_date')::date >= current_date`, orgID).Scan(&out.OpenFollowUps); err != nil {
		return app.OrgStatsView{}, err
	}
	err = q.pool.QueryRow(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(e.recorded_at)), 0)::float8
		  FROM es.events e
		 WHERE e.global_position > COALESCE(
		   (SELECT last_position FROM platform.projection_checkpoints WHERE projection_name = $1), 0)`,
		app.ProjectionOrgStats).Scan(&out.LagSeconds)
	if out.LagSeconds < 0 {
		out.LagSeconds = 0
	}
	return out, err
}

func (q *StatsQueries) month(ctx context.Context, orgID uuid.UUID, month string) (app.MonthStats, error) {
	ms := app.MonthStats{Month: month}
	var raw []byte
	err := q.pool.QueryRow(ctx, `
		SELECT metrics FROM read_partnernetwork.org_stats_monthly
		 WHERE organization_id = $1 AND month = ($2 || '-01')::date`, orgID, month).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ms, nil
	}
	if err != nil {
		return ms, err
	}
	var m map[string]int
	if err := json.Unmarshal(raw, &m); err != nil {
		return ms, err
	}
	ms.EncountersSigned, ms.Confirmed, ms.Completed = m[app.StatEncountersSigned], m[app.StatApptConfirmed], m[app.StatApptCompleted]
	ms.Cancelled, ms.NoShow = m[app.StatApptCancelled], m[app.StatApptNoShow]
	ms.NewPatients, ms.Vaccinations = m[app.StatNewPatients], m[app.StatVaccinations]
	return ms, nil
}
