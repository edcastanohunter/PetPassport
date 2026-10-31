package app

import (
	"context"

	"github.com/google/uuid"
)

// Estadísticas básicas de clínica (PP-F2-PRD-026, API-011): contadores
// mensuales de una proyección asíncrona (`partnernetwork.org_stats`).

// ProjectionOrgStats es el nombre de la suscripción (checkpoint).
const ProjectionOrgStats = "partnernetwork.org_stats"

// Claves de los contadores en org_stats_monthly.metrics.
const (
	StatEncountersSigned = "encounters_signed"
	StatApptConfirmed    = "appt_confirmed"
	StatApptCompleted    = "appt_completed"
	StatApptCancelled    = "appt_cancelled"
	StatApptNoShow       = "appt_no_show"
	StatNewPatients      = "new_patients"
	StatVaccinations     = "vaccinations"
)

// MonthStats son los contadores de un mes (UTC).
type MonthStats struct {
	Month            string // YYYY-MM
	EncountersSigned int
	Confirmed        int
	Completed        int
	Cancelled        int
	NoShow           int
	NewPatients      int
	Vaccinations     int
}

// OrgStatsView es la respuesta de GET /organizations/{orgId}/stats.
type OrgStatsView struct {
	Current       MonthStats
	Previous      MonthStats
	OpenFollowUps int
	LagSeconds    float64
}

// StatsQueries es el lado de consulta de las estadísticas.
type StatsQueries interface {
	// OrgStats devuelve el mes pedido (YYYY-MM) y el anterior.
	OrgStats(ctx context.Context, orgID uuid.UUID, month string) (OrgStatsView, error)
}
