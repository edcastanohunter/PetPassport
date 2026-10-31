// Command migrate aplica las migraciones goose embebidas (docs/phase-1/06 §2).
//
//	migrate up | down | status | version | seed-ref
//
// Se ejecuta con el rol pp_migrator (PP_DB_URL), nunca con el de la aplicación.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/edcastanohunter/petpassport/backend/migrations"
	refseed "github.com/edcastanohunter/petpassport/backend/seeds/ref"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	url := os.Getenv("PP_DB_URL")
	if url == "" {
		return fmt.Errorf("PP_DB_URL es obligatoria")
	}
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	// goose crea su tabla de versiones antes de la primera migración: el esquema
	// platform debe existir ya (docs/phase-1/06 §1).
	if _, err := sqlDB.ExecContext(context.Background(), "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		return fmt.Errorf("ensure platform schema: %w", err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("platform.schema_migrations")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	ctx := context.Background()
	switch cmd {
	case "up":
		if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
			return err
		}
		return seedRef(ctx, sqlDB)
	case "seed-ref":
		return seedRef(ctx, sqlDB)
	case "down":
		return goose.DownContext(ctx, sqlDB, ".")
	case "status":
		return goose.StatusContext(ctx, sqlDB, ".")
	case "version":
		return goose.VersionContext(ctx, sqlDB, ".")
	}
	return fmt.Errorf("comando desconocido: %s (up|down|status|version|seed-ref)", cmd)
}

// seedRef aplica los catálogos de referencia (DAT-009). Los registros solo se
// activan si APPROVALS.md los aprueba (approved: true en los YAML); en
// desarrollo PP_SEED_UNAPPROVED=1 los activa igualmente, nunca en staging/prod.
func seedRef(ctx context.Context, db *sql.DB) error {
	unapproved := os.Getenv("PP_SEED_UNAPPROVED") == "1"
	if env := os.Getenv("PP_ENV"); unapproved && (env == "staging" || env == "prod") {
		return fmt.Errorf("PP_SEED_UNAPPROVED=1 no está permitido en %s (APPROVALS.md)", env)
	}
	n, err := refseed.Apply(ctx, func(ctx context.Context, q string, args ...any) error {
		_, err := db.ExecContext(ctx, q, args...)
		return err
	}, unapproved)
	if err != nil {
		return err
	}
	slog.Info("catálogos de referencia aplicados", "filas", n, "activar_no_aprobados", unapproved)
	return nil
}
