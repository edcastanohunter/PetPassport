// Command api es el servidor HTTP de PetPassport. Secuencia de arranque y apagado
// según docs/phase-1/03-arquitectura.md §8.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry"
)

// version se inyecta en build con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("api terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Configuración (falla rápido).
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 2. Telemetría.
	tel, err := telemetry.Init(ctx, telemetry.Options{
		ServiceName: "petpassport-api", ServiceVersion: version, Env: string(cfg.Env),
		LogLevel: cfg.LogLevel, OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()
	logger := tel.Logger
	logger.Info("starting", "http_addr", cfg.HTTPAddr, "internal_addr", cfg.InternalAddr)

	// 3. Base de datos (pool + SELECT 1).
	pool, err := db.Open(ctx, cfg.DB.URL, cfg.DB.MaxConns, tel.Registry)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 4. Event store registry — M1.2. 5. OIDC verifier — M1.1.
	// 6. Módulos (identity, partnernetwork, petregistry) — M1.1+.

	// 7. Router y servidores.
	health := httpserver.NewHealth()
	health.Register("db", pool.Ping)
	servers := httpserver.New(cfg.HTTPAddr, cfg.InternalAddr, httpserver.Options{
		Logger: logger, Registry: tel.Registry, Health: health,
		CORSOrigins: cfg.CORSOrigins, Dev: cfg.IsDev(),
		Mount: func(r chi.Router) {
			// Las rutas de negocio se montan aquí a partir de M1.1.
		},
	})
	errs := servers.Start()
	health.SetReady(true)
	logger.Info("ready")

	// Apagado ordenado: readyz=fail → drenado → Shutdown → cierre del pool (defer).
	select {
	case err := <-errs:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	health.SetReady(false)
	time.Sleep(cfg.ShutdownDrain)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := servers.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}
