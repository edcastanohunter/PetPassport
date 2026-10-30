package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// CatalogAdmin es la escritura de catálogos por el super_admin (API-005,
// DOM-009): CRUD auditado con motivo. Desactivar es baja lógica.
type CatalogAdmin interface {
	SaveVaccine(ctx context.Context, actor uuid.UUID, id *uuid.UUID, v VaccineRef, reason string) (VaccineRef, error)
	DeactivateVaccine(ctx context.Context, actor, id uuid.UUID, reason string) error
	SaveProtocol(ctx context.Context, actor uuid.UUID, id *uuid.UUID, p ProtocolRef, reason string) (ProtocolRef, error)
	DeactivateProtocol(ctx context.Context, actor, id uuid.UUID, reason string) error
}

// CardFingerprint es el hash de contenido del carnet: SHA-256 de las
// entradas activas (id, tipo, producto, fecha, lote, verificación) en orden
// estable. Lo lleva el QR del PDF y /verify/cards lo recomputa (API-013).
func CardFingerprint(entries []CardEntryView) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Status != "active" {
			continue
		}
		lot := ""
		if e.Lot != nil {
			lot = *e.Lot
		}
		lines = append(lines, strings.Join([]string{
			e.ID.String(), e.Kind, strings.ToLower(e.ProductName), e.AdministeredAt, lot, e.Verification,
		}, "|"))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
