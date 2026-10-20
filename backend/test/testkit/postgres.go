// Package testkit ofrece utilidades para pruebas de integración:
// PostgreSQL real con testcontainers y migraciones aplicadas, y un servidor
// JWKS de prueba para firmar tokens sin Keycloak (docs/phase-1/10 §3).
package testkit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/edcastanohunter/petpassport/backend/migrations"
)

// Postgres arranca un contenedor postgres:17 y aplica las migraciones.
// Devuelve la URL de conexión (rol superusuario del contenedor, que actúa
// como pp_migrator en pruebas).
func Postgres(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("petpassport"),
		tcpostgres.WithUsername("pp_migrator"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	Migrate(t, url)
	return url
}

// Migrate aplica todas las migraciones embebidas sobre la URL dada.
func Migrate(t testing.TB, url string) {
	t.Helper()
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.ExecContext(context.Background(), "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("platform.schema_migrations")
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(context.Background(), sqlDB, "."); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

// MigrateDown revierte la última migración (para la prueba up/down/up de CI).
func MigrateDown(t testing.TB, url string) {
	t.Helper()
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("platform.schema_migrations")
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownContext(context.Background(), sqlDB, "."); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
}
