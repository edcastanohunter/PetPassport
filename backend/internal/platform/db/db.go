// Package db gestiona el pool de conexiones PostgreSQL y la unidad de trabajo
// transaccional usada por el command bus (docs/phase-1/03 §3). En M1.0 solo
// expone el pool, el ping de readiness y Do(); el UoW completo llega en M1.2.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// Pool envuelve pgxpool con las métricas del proyecto.
type Pool struct {
	*pgxpool.Pool
}

// Open crea el pool, aplica los límites y verifica la conexión con SELECT 1.
func Open(ctx context.Context, url string, maxConns int32, reg prometheus.Registerer) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	// Timeouts de sesión según docs/phase-1/06 §1.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["application_name"] = "petpassport-api"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ping(pingCtx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if reg != nil {
		reg.MustRegister(newCollector(pool))
	}
	return &Pool{Pool: pool}, nil
}

// Ping ejecuta SELECT 1; lo usa /readyz.
func (p *Pool) Ping(ctx context.Context) error { return ping(ctx, p.Pool) }

func ping(ctx context.Context, pool *pgxpool.Pool) error {
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}
	return nil
}

// Do ejecuta fn dentro de una transacción; confirma si fn devuelve nil,
// revierte en caso contrario o si hay panic.
func (p *Pool) Do(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
