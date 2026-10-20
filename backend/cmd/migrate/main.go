// Command migrate aplica las migraciones goose embebidas (docs/phase-1/06 §2).
//
//	migrate up | down | status | version
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
		return goose.UpContext(ctx, sqlDB, ".")
	case "down":
		return goose.DownContext(ctx, sqlDB, ".")
	case "status":
		return goose.StatusContext(ctx, sqlDB, ".")
	case "version":
		return goose.VersionContext(ctx, sqlDB, ".")
	}
	return fmt.Errorf("comando desconocido: %s (up|down|status|version)", cmd)
}
