package migrations_test

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/edcastanohunter/petpassport/backend/migrations"
)

// TestMigrations_ValidSyntax ensures all embedded SQL migrations conform to Goose syntax
// and contain valid +goose annotations, preventing CI/integration failures.
func TestMigrations_ValidSyntax(t *testing.T) {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	migrationsList, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		t.Fatalf("failed to collect and parse migrations: %v", err)
	}

	if len(migrationsList) == 0 {
		t.Fatal("no migrations found in embedded filesystem")
	}

	for _, m := range migrationsList {
		if m.Source == "" {
			t.Errorf("migration version %d has empty source", m.Version)
		}
	}
}
