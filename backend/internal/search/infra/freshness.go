package infra

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBFreshnessProvider consulta el lag de indexación comparando el último evento con el checkpoint de búsqueda.
type DBFreshnessProvider struct {
	pool *pgxpool.Pool
}

// NewDBFreshnessProvider crea un nuevo DBFreshnessProvider.
func NewDBFreshnessProvider(pool *pgxpool.Pool) *DBFreshnessProvider {
	return &DBFreshnessProvider{pool: pool}
}

// GetLagSeconds calcula el retraso en segundos del índice respecto al último evento.
func (f *DBFreshnessProvider) GetLagSeconds(ctx context.Context, indexName string) int64 {
	projectionName := "search." + indexName
	var lagSeconds float64

	// Obtener diferencia de tiempo entre el último evento del store y el último evento procesado por el checkpoint
	query := `
		SELECT EXTRACT(EPOCH FROM (COALESCE(MAX(e.created_at), now()) - COALESCE(c.updated_at, now())))
		FROM es.events e
		LEFT JOIN es.checkpoints c ON c.name = $1
		WHERE e.global_position = (SELECT COALESCE(MAX(global_position), 0) FROM es.events)
		GROUP BY c.updated_at
	`
	err := f.pool.QueryRow(ctx, query, projectionName).Scan(&lagSeconds)
	if err != nil || lagSeconds < 0 {
		return 0
	}
	return int64(lagSeconds)
}

// MockFreshnessProvider para pruebas
type MockFreshnessProvider struct {
	Lag int64
}

func (m *MockFreshnessProvider) GetLagSeconds(ctx context.Context, index string) int64 {
	return m.Lag
}
