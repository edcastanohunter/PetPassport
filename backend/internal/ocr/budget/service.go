package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// Service implementa ocrdomain.BudgetService sobre PostgreSQL (PP-F4-OCR-008, DAT-004).
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// CheckBudget verifica el estado del presupuesto mensual y alerta o corte duro (PP-F4-OCR-008).
func (s *Service) CheckBudget(ctx context.Context) (ocrdomain.BudgetStatus, error) {
	if s.pool == nil {
		// Modo mock/sin BD: permitir
		return ocrdomain.BudgetStatus{
			MonthlyUSD: 300.0,
			CurrentUSD: 0.0,
			HardStop:   true,
			AlertPct:   80,
		}, nil
	}

	// 1. Obtener límites de ocr.budget
	var monthlyUSD float64
	var hardStop bool
	var alertPct int
	err := s.pool.QueryRow(ctx, `
		SELECT monthly_usd, hard_stop, alert_pct
		FROM ocr.budget
		WHERE id = 1
	`).Scan(&monthlyUSD, &hardStop, &alertPct)
	if err != nil {
		// Si no existe, valores por defecto
		monthlyUSD = 300.00
		hardStop = true
		alertPct = 80
	}

	// 2. Sumar consumo del mes actual desde ocr.ai_usage_daily
	var currentUSD float64
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_usd), 0.0)
		FROM ocr.ai_usage_daily
		WHERE day >= date_trunc('month', CURRENT_DATE)
	`).Scan(&currentUSD)
	if err != nil {
		return ocrdomain.BudgetStatus{}, fmt.Errorf("consultar consumo de IA: %w", err)
	}

	exceeded := currentUSD >= monthlyUSD
	alertTriggered := (currentUSD / monthlyUSD * 100.0) >= float64(alertPct)

	status := ocrdomain.BudgetStatus{
		MonthlyUSD:     monthlyUSD,
		CurrentUSD:     currentUSD,
		HardStop:       hardStop,
		AlertPct:       alertPct,
		Exceeded:       exceeded,
		AlertTriggered: alertTriggered,
	}

	if exceeded && hardStop {
		return status, ocrdomain.ErrBudgetExceeded
	}

	return status, nil
}

// RecordUsage incrementa las métricas de consumo de tokens y coste diario (PP-F4-OCR-008, DAT-004).
func (s *Service) RecordUsage(ctx context.Context, day time.Time, provider string, model string, inTokens int64, outTokens int64, costUSD float64, refused bool) error {
	if s.pool == nil {
		return nil
	}

	dateOnly := day.Format("2006-01-02")
	refusalInc := 0
	if refused {
		refusalInc = 1
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO ocr.ai_usage_daily (
			day, provider, model, requests, input_tokens, output_tokens, cost_usd, refusals
		) VALUES ($1, $2, $3, 1, $4, $5, $6, $7)
		ON CONFLICT (day, provider, model) DO UPDATE SET
			requests = ocr.ai_usage_daily.requests + 1,
			input_tokens = ocr.ai_usage_daily.input_tokens + EXCLUDED.input_tokens,
			output_tokens = ocr.ai_usage_daily.output_tokens + EXCLUDED.output_tokens,
			cost_usd = ocr.ai_usage_daily.cost_usd + EXCLUDED.cost_usd,
			refusals = ocr.ai_usage_daily.refusals + EXCLUDED.refusals
	`, dateOnly, provider, model, inTokens, outTokens, costUSD, refusalInc)
	if err != nil {
		return fmt.Errorf("registrar uso diario de IA: %w", err)
	}

	return nil
}

var _ ocrdomain.BudgetService = (*Service)(nil)
