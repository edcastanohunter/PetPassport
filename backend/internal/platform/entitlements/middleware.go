package entitlements

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// EnforceEntitlements crea el middleware del command bus para verificar límites de plan (PP-F5-ARQ-004).
func EnforceEntitlements(client kernel.Entitlements) bus.Middleware {
	return func(next bus.Handler) bus.Handler {
		return func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
			// Comandos exentos: Billing e Identity (RN-32, ARQ-004)
			name := cmd.CommandName()
			if strings.HasPrefix(name, "billing.") || strings.HasPrefix(name, "identity.") {
				return next(ctx, cmd)
			}

			// Si el comando declara consumo de recursos o capacidades
			consuming, ok := cmd.(kernel.ConsumingCommand)
			if !ok {
				return next(ctx, cmd)
			}

			orgID := consuming.TargetOrgID()
			if orgID == uuid.Nil {
				return next(ctx, cmd)
			}

			set, err := client.For(ctx, orgID)
			if err != nil {
				return bus.Result{}, fmt.Errorf("entitlements: %w", err)
			}

			// Suscripción restringida o expirada bloquea todos los comandos de escritura (RN-32, ARQ-004)
			if set.Status == kernel.EntitlementRestricted || set.Status == kernel.EntitlementExpired {
				return bus.Result{}, RestrictedError{
					Reason:     "La organización tiene la suscripción restringida. Actualice su suscripción para operar.",
					BillingURL: fmt.Sprintf("/org/%s/settings/billing", orgID.String()),
				}
			}

			req := consuming.Consumes()
			switch req.Kind {
			case kernel.ReqKindSeat:
				if set.Seats.Limit > 0 && set.Seats.Used+int(req.Amount) > set.Seats.Limit {
					return bus.Result{}, PlanLimitReachedError{
						Quota:      "seats",
						Limit:      int64(set.Seats.Limit),
						Used:       int64(set.Seats.Used),
						PeriodEnd:  set.PeriodEnd,
						UpgradeURL: fmt.Sprintf("/org/%s/settings/billing", orgID.String()),
					}
				}
			case kernel.ReqKindQuota:
				if q, found := set.Quotas[kernel.QuotaKey(req.Key)]; found && q.Limit > 0 {
					softLimit := q.SoftLimit
					if softLimit == 0 {
						softLimit = int64(float64(q.Limit) * 1.10) // 10% límite blando (ADR-025, ARQ-004)
					}
					if q.Used+req.Amount > softLimit {
						return bus.Result{}, PlanLimitReachedError{
							Quota:      req.Key,
							Limit:      q.Limit,
							Used:       q.Used,
							PeriodEnd:  q.PeriodEnd,
							UpgradeURL: fmt.Sprintf("/org/%s/settings/billing", orgID.String()),
						}
					}
				}
			case kernel.ReqKindFeature:
				if !set.HasFeature(req.Key) {
					return bus.Result{}, PlanLimitReachedError{
						Quota:      "feature:" + req.Key,
						Limit:      1,
						Used:       0,
						PeriodEnd:  set.PeriodEnd,
						UpgradeURL: fmt.Sprintf("/org/%s/settings/billing", orgID.String()),
					}
				}
			}

			return next(ctx, cmd)
		}
	}
}
