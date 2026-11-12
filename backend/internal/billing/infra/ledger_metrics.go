package infra

import (
	"context"
	"fmt"
	"time"
)

// RenewalsOverdue cuenta las suscripciones cuyo periodo venció antes de before y
// siguen sin renovar (alerta RenewalJobStalled, INF-004).
func (l *Ledger) RenewalsOverdue(ctx context.Context, before time.Time) (int, error) {
	var n int
	if err := l.q(ctx).QueryRow(ctx, `
SELECT COUNT(*) FROM billing.subscriptions
WHERE status IN ('active','grace','restricted') AND period_end < $1`, before.UTC()).Scan(&n); err != nil {
		return 0, fmt.Errorf("billing renewals overdue: %w", err)
	}
	return n, nil
}

// MRR devuelve el ingreso recurrente mensual por moneda (INF-003): la base del
// último cargo de periodo cobrado de cada suscripción al día, prorrateada a un mes.
func (l *Ledger) MRR(ctx context.Context) (map[string]float64, error) {
	rows, err := l.q(ctx).Query(ctx, `
SELECT c.currency, COALESCE(SUM(CASE WHEN s.cycle = 'annual' THEN c.amount / 12 ELSE c.amount END), 0)::float8
FROM billing.subscriptions s
JOIN LATERAL (
  SELECT amount, currency FROM billing.charges
  WHERE subscription_id = s.id AND period_start IS NOT NULL AND status IN ('paid','partially_refunded')
  ORDER BY scheduled_at DESC LIMIT 1
) c ON true
WHERE s.status IN ('active','grace')
GROUP BY c.currency`)
	if err != nil {
		return nil, fmt.Errorf("billing mrr: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var (
			currency string
			amount   float64
		)
		if err := rows.Scan(&currency, &amount); err != nil {
			return nil, fmt.Errorf("billing mrr scan: %w", err)
		}
		out[currency] = amount
	}
	return out, rows.Err()
}
