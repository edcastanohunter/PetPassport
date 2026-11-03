package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	goredis "github.com/redis/go-redis/v9"
)

var (
	metricScrapingAlerts = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "security",
		Name:      "scraping_alerts_total",
		Help:      "Total de alertas de scraping detectadas en búsqueda.",
	})
)

// AbuseDetector supervisa patrones anómalos de búsqueda y scraping (SEC-004).
type AbuseDetector struct {
	redis  *goredis.Client
	logger *slog.Logger
}

// NewAbuseDetector crea un nuevo AbuseDetector.
func NewAbuseDetector(redis *goredis.Client, logger *slog.Logger) *AbuseDetector {
	return &AbuseDetector{
		redis:  redis,
		logger: logger,
	}
}

// TrackQuery registra una consulta de búsqueda realizada por un usuario y detecta si sobrepasa límites de abuso.
func (d *AbuseDetector) TrackQuery(ctx context.Context, userID uuid.UUID, patientIDs []string) {
	if d.redis == nil || userID == uuid.Nil {
		return
	}

	today := time.Now().UTC().Format("2006-01-02")
	queryCountKey := fmt.Sprintf("pp:abuse:queries:%s:%s", userID, today)
	patientsSetKey := fmt.Sprintf("pp:abuse:patients:%s:%s", userID, today)

	pipe := d.redis.Pipeline()
	queriesCmd := pipe.Incr(ctx, queryCountKey)
	pipe.Expire(ctx, queryCountKey, 48*time.Hour)

	if len(patientIDs) > 0 {
		var members []any
		for _, id := range patientIDs {
			members = append(members, id)
		}
		pipe.SAdd(ctx, patientsSetKey, members...)
		pipe.Expire(ctx, patientsSetKey, 48*time.Hour)
	}
	patientsCountCmd := pipe.SCard(ctx, patientsSetKey)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return
	}

	queries := queriesCmd.Val()
	patients := patientsCountCmd.Val()

	// Umbrales SEC-004: > 2.000 consultas/día o > 500 pacientes distintos/día
	if queries > 2000 || patients > 500 {
		metricScrapingAlerts.Inc()
		d.logger.WarnContext(ctx, "potential search scraping detected",
			"user_id", userID,
			"queries_today", queries,
			"distinct_patients_today", patients,
		)
	}
}
