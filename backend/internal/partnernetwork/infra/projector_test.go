package infra

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func TestTranslateUnique_TaxIDAlreadyRegistered_DAT006(t *testing.T) {
	err := translateUnique(fmt.Errorf("projection: %w", &pgconn.PgError{Code: "23505", ConstraintName: "organizations_tax_idx"}))
	var ve *kernel.ValidationError
	if !errors.As(err, &ve) || ve.Fields[0].Field != "tax_id" || ve.Fields[0].Code != "ALREADY_REGISTERED" {
		t.Fatalf("translateUnique: %v", err)
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "otra"}
	if translateUnique(other) != error(other) || translateUnique(nil) != nil {
		t.Fatal("otros errores pasan tal cual")
	}
}
