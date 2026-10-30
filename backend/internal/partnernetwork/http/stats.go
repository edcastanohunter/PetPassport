package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var monthRE = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// StatsHandlers implementa GET /organizations/{orgId}/stats (PRD-026).
type StatsHandlers struct {
	queries app.StatsQueries
	logger  *slog.Logger
	now     func() time.Time
}

// NewStatsHandlers construye los handlers.
func NewStatsHandlers(queries app.StatsQueries, logger *slog.Logger) *StatsHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &StatsHandlers{queries: queries, logger: logger, now: time.Now}
}

func monthDTO(m app.MonthStats) gen.MonthStats {
	out := gen.MonthStats{
		Month: m.Month, EncountersSigned: m.EncountersSigned, NewPatients: m.NewPatients, Vaccinations: m.Vaccinations,
	}
	out.Appointments.Confirmed, out.Appointments.Completed = m.Confirmed, m.Completed
	out.Appointments.Cancelled, out.Appointments.NoShow = m.Cancelled, m.NoShow
	return out
}

// GetOrgStats devuelve los contadores del mes pedido y del anterior.
func (h *StatsHandlers) GetOrgStats(ctx context.Context, req gen.GetOrgStatsRequestObject) (gen.GetOrgStatsResponseObject, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return nil, &kernel.ForbiddenError{Reason: "sin principal en el contexto"}
	}
	orgID := uuid.UUID(req.OrgId)
	if d := authz.Can(p, authz.OrgStatsRead, authz.OrgResource{ID: orgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	month := h.now().UTC().Format("2006-01")
	if req.Params.Month != nil {
		if !monthRE.MatchString(*req.Params.Month) {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "month", Code: "INVALID"}}}
		}
		month = *req.Params.Month
	}
	v, err := h.queries.OrgStats(ctx, orgID, month)
	if err != nil {
		return nil, err
	}
	return gen.GetOrgStats200JSONResponse{
		Headers: gen.GetOrgStats200ResponseHeaders{XPPFreshness: ptr("lag=" + formatLag(v.LagSeconds))},
		Body: gen.OrgStats{
			Current: monthDTO(v.Current), Previous: monthDTO(v.Previous), OpenFollowUps: v.OpenFollowUps,
			Freshness: gen.Freshness{LagSeconds: float32(v.LagSeconds)},
		},
	}, nil
}

func formatLag(lag float64) string {
	b, _ := json.Marshal(float64(int64(lag*10)) / 10)
	return string(b)
}

func ptr[T any](v T) *T { return &v }
