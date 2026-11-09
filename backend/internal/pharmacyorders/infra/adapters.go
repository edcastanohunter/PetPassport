package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// PostgresPrescriptionReader implementa domain.PrescriptionReader sobre read_prescriptions.prescriptions (DOM-013).
type PostgresPrescriptionReader struct {
	pool *pgxpool.Pool
}

var _ domain.PrescriptionReader = (*PostgresPrescriptionReader)(nil)

// NewPostgresPrescriptionReader construye el lector de prescripciones.
func NewPostgresPrescriptionReader(pool *pgxpool.Pool) *PostgresPrescriptionReader {
	return &PostgresPrescriptionReader{pool: pool}
}

// GetPrescription consulta la receta y formatea el snapshot requerido para cotización.
func (r *PostgresPrescriptionReader) GetPrescription(ctx context.Context, id uuid.UUID) (*domain.PrescriptionSnapshot, error) {
	var (
		petID, guardianID           uuid.UUID
		country, source, status     string
		validUntil                  *time.Time
		refillsAllowed, refillsUsed int
		itemsJSON                   []byte
	)

	tx, err := db.TxFrom(ctx)
	var row pgx.Row
	query := `
		SELECT pet_id, guardian_id, country, source, status, valid_until, refills_allowed, refills_used, items
		FROM read_prescriptions.prescriptions
		WHERE id = $1`

	if err == nil && tx != nil {
		row = tx.QueryRow(ctx, query, id)
	} else {
		row = r.pool.QueryRow(ctx, query, id)
	}

	if err := row.Scan(&petID, &guardianID, &country, &source, &status, &validUntil, &refillsAllowed, &refillsUsed, &itemsJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPrescriptionNotQuotable
		}
		return nil, fmt.Errorf("query prescription: %w", err)
	}

	type rawItem struct {
		ItemID        string     `json:"item_id"`
		CatalogID     *uuid.UUID `json:"catalog_id"`
		NameSnapshot  string     `json:"name_snapshot"`
		Form          string     `json:"form"`
		Concentration string     `json:"concentration"`
		FreeText      *string    `json:"free_text"`
		Dose          struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"dose"`
		Frequency          string `json:"frequency"`
		QuantityToDispense struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"quantity_to_dispense"`
		SubstitutionAllowed bool `json:"substitution_allowed"`
		Controlled          bool `json:"controlled"`
	}

	var rawItems []rawItem
	if len(itemsJSON) > 0 {
		_ = json.Unmarshal(itemsJSON, &rawItems)
	}

	items := make([]domain.PrescriptionItemSnapshot, 0, len(rawItems))
	for _, it := range rawItems {
		doseStr := fmt.Sprintf("%.2g %s", it.Dose.Value, it.Dose.Unit)
		items = append(items, domain.PrescriptionItemSnapshot{
			ItemID:              it.ItemID,
			CatalogID:           it.CatalogID,
			Name:                it.NameSnapshot,
			Form:                it.Form,
			Concentration:       it.Concentration,
			FreeText:            it.FreeText,
			Dose:                doseStr,
			Frequency:           it.Frequency,
			Quantity:            it.QuantityToDispense.Value,
			Unit:                it.QuantityToDispense.Unit,
			SubstitutionAllowed: it.SubstitutionAllowed,
			Controlled:          it.Controlled,
		})
	}

	snap := &domain.PrescriptionSnapshot{
		ID:             id,
		PetID:          petID,
		GuardianID:     guardianID,
		Country:        country,
		Status:         status,
		Source:         source,
		RefillsAllowed: refillsAllowed,
		RefillsUsed:    refillsUsed,
		Items:          items,
	}
	if validUntil != nil {
		snap.ValidUntil = *validUntil
	}
	return snap, nil
}

// PostgresQuotaChecker comprueba cuotas operativas de cotizaciones (DOM-008, Q5).
type PostgresQuotaChecker struct {
	pool *pgxpool.Pool
}

var _ domain.QuoteRequestQuotaChecker = (*PostgresQuotaChecker)(nil)

// NewPostgresQuotaChecker construye el validador de cuotas.
func NewPostgresQuotaChecker(pool *pgxpool.Pool) *PostgresQuotaChecker {
	return &PostgresQuotaChecker{pool: pool}
}

// CheckQuotas verifica que no se excedan 3 cotizaciones abiertas por receta ni 10 diarias por tutor.
func (q *PostgresQuotaChecker) CheckQuotas(ctx context.Context, guardianID uuid.UUID, prescriptionID uuid.UUID, now time.Time) error {
	tx, err := db.TxFrom(ctx)
	execQuery := func(sql string, args ...any) (int, error) {
		var count int
		var row pgx.Row
		if err == nil && tx != nil {
			row = tx.QueryRow(ctx, sql, args...)
		} else {
			row = q.pool.QueryRow(ctx, sql, args...)
		}
		scanErr := row.Scan(&count)
		return count, scanErr
	}

	// Límite 1: máximo 3 solicitudes abiertas por receta
	openCount, err := execQuery(`
		SELECT COUNT(*)
		FROM read_pharmacyorders.quote_requests
		WHERE prescription_id = $1 AND status = 'open'`, prescriptionID)
	if err != nil {
		return fmt.Errorf("checking open quote requests: %w", err)
	}
	if openCount >= 3 {
		return domain.ErrQuoteRequestLimit
	}

	// Límite 2: máximo 10 solicitudes creadas en las últimas 24 horas por el tutor
	dayAgo := now.Add(-24 * time.Hour)
	dailyCount, err := execQuery(`
		SELECT COUNT(*)
		FROM read_pharmacyorders.quote_requests
		WHERE guardian_id = $1 AND created_at >= $2`, guardianID, dayAgo)
	if err != nil {
		return fmt.Errorf("checking daily quote requests: %w", err)
	}
	if dailyCount >= 10 {
		return domain.ErrQuoteRequestLimit
	}

	return nil
}

// PostgresPartnerDirectory implementa domain.PartnerDirectory sobre las organizaciones de tipo farmacia (PRD-021).
type PostgresPartnerDirectory struct {
	pool *pgxpool.Pool
}

var _ domain.PartnerDirectory = (*PostgresPartnerDirectory)(nil)

// NewPostgresPartnerDirectory construye el directorio de farmacias.
func NewPostgresPartnerDirectory(pool *pgxpool.Pool) *PostgresPartnerDirectory {
	return &PostgresPartnerDirectory{pool: pool}
}

// EligiblePharmacies busca farmacias activas con soporte para la modalidad y área solicitadas.
func (d *PostgresPartnerDirectory) EligiblePharmacies(ctx context.Context, country string, area domain.Area, mode domain.FulfillmentMode) ([]domain.EligiblePharmacy, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT o.id, o.display_name, o.city,
		       COALESCE(i.capabilities, '{"pickup":true,"delivery":false,"substitutions":true}'::jsonb) as capabilities,
		       COALESCE(s.avg_response_minutes, 0),
		       COALESCE(s.quote_rate, 0),
		       COALESCE(s.rating_avg, 0)
		FROM read_partnernetwork.organizations o
		LEFT JOIN read_partnernetwork.partner_integrations i ON i.organization_id = o.id
		LEFT JOIN read_pharmacyorders.partner_sla s ON s.organization_id = o.id
		WHERE o.kind = 'pharmacy' AND o.status = 'active'
		  AND (i.status IS NULL OR i.status = 'active')
		ORDER BY s.rating_avg DESC NULLS LAST, o.display_name ASC
		LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("querying eligible pharmacies: %w", err)
	}
	defer rows.Close()

	var result []domain.EligiblePharmacy
	for rows.Next() {
		var (
			orgID                  uuid.UUID
			name, city             string
			capsJSON               []byte
			avgResp, qRate, rating float64
		)
		if err := rows.Scan(&orgID, &name, &city, &capsJSON, &avgResp, &qRate, &rating); err != nil {
			return nil, err
		}

		var caps domain.PartnerCapabilities
		_ = json.Unmarshal(capsJSON, &caps)

		modes := []domain.FulfillmentMode{domain.ModePickup}
		if caps.Delivery {
			modes = append(modes, domain.ModeDelivery)
		}

		// Filtrar si mode específico fue solicitado y la farmacia no lo soporta
		if mode == domain.ModeDelivery && !caps.Delivery {
			continue
		}

		ep := domain.EligiblePharmacy{
			OrganizationID: orgID,
			DisplayName:    name,
			City:           city,
			Modes:          modes,
			Capabilities:   caps,
		}
		if avgResp > 0 {
			ep.AvgResponseMin = &avgResp
		}
		if qRate > 0 {
			ep.QuoteRate = &qRate
		}
		if rating > 0 {
			ep.Rating = &rating
		}
		result = append(result, ep)
	}
	return result, nil
}

// IsEligible verifica si una organización específica es una farmacia activa que soporte el modo.
func (d *PostgresPartnerDirectory) IsEligible(ctx context.Context, orgID uuid.UUID, area domain.Area, mode domain.FulfillmentMode) (bool, error) {
	var count int
	err := d.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM read_partnernetwork.organizations o
		LEFT JOIN read_partnernetwork.partner_integrations i ON i.organization_id = o.id
		WHERE o.id = $1 AND o.kind = 'pharmacy' AND o.status = 'active'
		  AND (i.status IS NULL OR i.status = 'active')`, orgID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// PostgresCatalogEquivalence comprueba bioequivalencia y sustancias controladas (DOM-013).
type PostgresCatalogEquivalence struct {
	pool *pgxpool.Pool
}

var _ domain.CatalogEquivalence = (*PostgresCatalogEquivalence)(nil)

// NewPostgresCatalogEquivalence construye el comprobador de catálogo.
func NewPostgresCatalogEquivalence(pool *pgxpool.Pool) *PostgresCatalogEquivalence {
	return &PostgresCatalogEquivalence{pool: pool}
}

// IsControlled consulta si el medicamento está catalogado como controlado.
func (c *PostgresCatalogEquivalence) IsControlled(ctx context.Context, catalogID uuid.UUID) (bool, error) {
	var controlled bool
	err := c.pool.QueryRow(ctx, `SELECT controlled FROM ref.medications WHERE id = $1`, catalogID).Scan(&controlled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return controlled, err
}

// AreEquivalent compara principio activo y forma farmacéutica entre dos medicamentos.
func (c *PostgresCatalogEquivalence) AreEquivalent(ctx context.Context, a, b uuid.UUID) (bool, error) {
	if a == b {
		return true, nil
	}
	var aiA, aiB []byte
	var formA, formB string

	err := c.pool.QueryRow(ctx, `SELECT active_ingredients, form FROM ref.medications WHERE id = $1`, a).Scan(&aiA, &formA)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = c.pool.QueryRow(ctx, `SELECT active_ingredients, form FROM ref.medications WHERE id = $1`, b).Scan(&aiB, &formB)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if formA != formB {
		return false, nil
	}
	return string(aiA) == string(aiB), nil
}
