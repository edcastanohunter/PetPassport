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

	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PostgresCredentialCheck implementa domain.CredentialCheck contra identity.professional_credentials (PP-F4-DOM-013).
type PostgresCredentialCheck struct {
	pool *pgxpool.Pool
}

var _ domain.CredentialCheck = (*PostgresCredentialCheck)(nil)

// NewPostgresCredentialCheck construye el adaptador de credenciales.
func NewPostgresCredentialCheck(pool *pgxpool.Pool) *PostgresCredentialCheck {
	return &PostgresCredentialCheck{pool: pool}
}

// Verified consulta si el usuario tiene credencial profesional activa y verificada para el país.
func (c *PostgresCredentialCheck) Verified(ctx context.Context, userID uuid.UUID, country string) (*domain.CredentialRef, bool, error) {
	var regNumber string
	var verifiedAt *time.Time
	err := c.pool.QueryRow(ctx, `
		SELECT registry_number, verified_at
		FROM identity.professional_credentials
		WHERE user_id = $1 AND country = $2 AND status = 'verified'
		  AND (expires_at IS NULL OR expires_at > now())
		LIMIT 1`, userID, country).Scan(&regNumber, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ref := &domain.CredentialRef{
		Country:        country,
		RegistryNumber: regNumber,
	}
	if verifiedAt != nil {
		ref.VerifiedAt = *verifiedAt
	}
	return ref, true, nil
}

// PostgresEncounterPlanReader implementa domain.EncounterPlanReader sobre read_clinical.encounters (PP-F4-DOM-013).
type PostgresEncounterPlanReader struct {
	pool *pgxpool.Pool
}

var _ domain.EncounterPlanReader = (*PostgresEncounterPlanReader)(nil)

// NewPostgresEncounterPlanReader construye el lector de planes clínicos.
func NewPostgresEncounterPlanReader(pool *pgxpool.Pool) *PostgresEncounterPlanReader {
	return &PostgresEncounterPlanReader{pool: pool}
}

// SignedPlanItems extrae los medicamentos prescritos en el plan de un encuentro clínico firmado.
func (r *PostgresEncounterPlanReader) SignedPlanItems(ctx context.Context, encounterID uuid.UUID, petID uuid.UUID, orgID uuid.UUID) ([]domain.EncounterPlanItem, bool, error) {
	var planJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT plan
		FROM read_clinical.encounters
		WHERE id = $1 AND pet_id = $2 AND organization_id = $3 AND status = 'signed'`,
		encounterID, petID, orgID).Scan(&planJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	type planMedication struct {
		CatalogID      *uuid.UUID `json:"catalog_id"`
		MedicationName string     `json:"medication_name"`
		Dose           float64    `json:"dose"`
		DoseUnit       string     `json:"dose_unit"`
		Route          string     `json:"route"`
		Frequency      struct {
			Once  bool   `json:"once"`
			Every int    `json:"every"`
			Unit  string `json:"unit"`
		} `json:"frequency"`
		DurationDays *int   `json:"duration_days"`
		Instructions string `json:"instructions"`
	}
	type planData struct {
		Medications []planMedication `json:"medications"`
	}

	var data planData
	if err := json.Unmarshal(planJSON, &data); err != nil {
		return nil, false, fmt.Errorf("unmarshal encounter plan: %w", err)
	}

	items := make([]domain.EncounterPlanItem, 0, len(data.Medications))
	for _, m := range data.Medications {
		freqStr := ""
		if m.Frequency.Once {
			freqStr = "dosis única"
		} else if m.Frequency.Every > 0 && m.Frequency.Unit != "" {
			freqStr = fmt.Sprintf("cada %d %s", m.Frequency.Every, m.Frequency.Unit)
		}
		items = append(items, domain.EncounterPlanItem{
			CatalogID:    m.CatalogID,
			Name:         m.MedicationName,
			DoseValue:    m.Dose,
			DoseUnit:     m.DoseUnit,
			Route:        m.Route,
			Frequency:    freqStr,
			DurationDays: m.DurationDays,
			Instructions: m.Instructions,
		})
	}
	return items, true, nil
}

// PostgresRegulatoryRules implementa domain.RegulatoryRules consultando ref.regulatory_rules (PP-F4-DAT-005, DOM-012).
type PostgresRegulatoryRules struct {
	pool *pgxpool.Pool
}

var _ domain.RegulatoryRules = (*PostgresRegulatoryRules)(nil)

// NewPostgresRegulatoryRules construye el proveedor de reglas regulatorias.
func NewPostgresRegulatoryRules(pool *pgxpool.Pool) *PostgresRegulatoryRules {
	return &PostgresRegulatoryRules{pool: pool}
}

// ForCountry devuelve las reglas de prescripción para un país.
func (r *PostgresRegulatoryRules) ForCountry(ctx context.Context, country string) (*domain.RegulatoryRule, error) {
	var rule domain.RegulatoryRule
	err := r.pool.QueryRow(ctx, `
		SELECT country, default_validity_days, max_validity_days, max_refills, controlled_orderable, requires_credential_verification
		FROM ref.regulatory_rules
		WHERE country = $1`, country).Scan(
		&rule.Country,
		&rule.DefaultValidityDays,
		&rule.MaxValidityDays,
		&rule.MaxRefills,
		&rule.ControlledOrderable,
		&rule.RequiresCredentialVerification,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Reglas por defecto si el país aún no está en catálogo
		return &domain.RegulatoryRule{
			Country:                        country,
			DefaultValidityDays:            30,
			MaxValidityDays:                180,
			MaxRefills:                     5,
			ControlledOrderable:            false,
			RequiresCredentialVerification: true,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query regulatory rules: %w", err)
	}
	return &rule, nil
}

// PostgresCatalogEquivalence implementa domain.CatalogEquivalence consultando ref.medications (PP-F4-DOM-013).
type PostgresCatalogEquivalence struct {
	pool *pgxpool.Pool
}

var _ domain.CatalogEquivalence = (*PostgresCatalogEquivalence)(nil)

// NewPostgresCatalogEquivalence construye el comprobador de equivalencias y controlados.
func NewPostgresCatalogEquivalence(pool *pgxpool.Pool) *PostgresCatalogEquivalence {
	return &PostgresCatalogEquivalence{pool: pool}
}

// IsControlled comprueba si un medicamento del catálogo es de control especial.
func (c *PostgresCatalogEquivalence) IsControlled(ctx context.Context, catalogID uuid.UUID) (bool, error) {
	var controlled bool
	err := c.pool.QueryRow(ctx, `SELECT controlled FROM ref.medications WHERE id = $1`, catalogID).Scan(&controlled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return controlled, err
}

// AreEquivalent determina si dos ítems de catálogo son bioequivalentes (mismo principio activo y forma farmacéutica).
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

// PostgresPetAccessChecker implementa domain.PetAccessChecker verificando care links y custodia clínica (PP-F4-DOM-013).
type PostgresPetAccessChecker struct {
	pool *pgxpool.Pool
}

var _ domain.PetAccessChecker = (*PostgresPetAccessChecker)(nil)

// NewPostgresPetAccessChecker construye el verificador de acceso clínico de una org a la mascota.
func NewPostgresPetAccessChecker(pool *pgxpool.Pool) *PostgresPetAccessChecker {
	return &PostgresPetAccessChecker{pool: pool}
}

// HasActiveAccess comprueba si la organización tiene relación activa o custodia clínica sobre la mascota.
func (p *PostgresPetAccessChecker) HasActiveAccess(ctx context.Context, orgID uuid.UUID, petID uuid.UUID) (bool, error) {
	var active bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM read_petregistry.pet_care_links l
			JOIN read_partnernetwork.organizations o ON o.id = l.organization_id
			WHERE l.pet_id = $1 AND l.organization_id = $2
			  AND l.status = 'active' AND l.org_suspended = false AND o.status = 'active'
		) OR EXISTS (
			SELECT 1 FROM read_clinical.encounter_custody c
			WHERE c.pet_id = $1 AND c.organization_id = $2
		)`, petID, orgID).Scan(&active)
	return active, err
}

// PostgresPetReader implementa app.PetAccessReader sobre read_petregistry.pets.
type PostgresPetReader struct {
	pool *pgxpool.Pool
}

func NewPostgresPetReader(pool *pgxpool.Pool) *PostgresPetReader {
	return &PostgresPetReader{pool: pool}
}

func (r *PostgresPetReader) GetPetInfo(ctx context.Context, petID uuid.UUID) (name string, species string, guardianID uuid.UUID, err error) {
	err = r.pool.QueryRow(ctx, `SELECT name, species, COALESCE(guardian_id, '00000000-0000-0000-0000-000000000000'::uuid) FROM read_petregistry.pets WHERE id = $1`, petID).Scan(&name, &species, &guardianID)
	return name, species, guardianID, err
}

// PostgresOrgReader implementa app.OrgNameReader sobre read_partnernetwork.organizations.
type PostgresOrgReader struct {
	pool *pgxpool.Pool
}

func NewPostgresOrgReader(pool *pgxpool.Pool) *PostgresOrgReader {
	return &PostgresOrgReader{pool: pool}
}

func (r *PostgresOrgReader) GetOrgName(ctx context.Context, orgID uuid.UUID) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT display_name FROM read_partnernetwork.organizations WHERE id = $1`, orgID).Scan(&name)
	return name, err
}

// PostgresUserReader implementa app.UserNameReader sobre identity.users.
type PostgresUserReader struct {
	pool *pgxpool.Pool
}

func NewPostgresUserReader(pool *pgxpool.Pool) *PostgresUserReader {
	return &PostgresUserReader{pool: pool}
}

func (r *PostgresUserReader) GetUserName(ctx context.Context, userID uuid.UUID) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT display_name FROM identity.users WHERE id = $1`, userID).Scan(&name)
	return name, err
}

// PostgresEncounterAccessReader implementa la lectura de acceso de un encuentro para autorización.
type PostgresEncounterAccessReader struct {
	pool *pgxpool.Pool
}

func NewPostgresEncounterAccessReader(pool *pgxpool.Pool) *PostgresEncounterAccessReader {
	return &PostgresEncounterAccessReader{pool: pool}
}

func (r *PostgresEncounterAccessReader) GetEncounterAccess(ctx context.Context, id uuid.UUID) (*authz.EncounterResource, error) {
	var enc authz.EncounterResource
	var guardianID *uuid.UUID
	enc.ID = id
	err := r.pool.QueryRow(ctx, `
		SELECT e.pet_id, e.organization_id, e.status, p.guardian_id
		FROM read_clinical.encounters e
		JOIN read_petregistry.pets p ON p.id = e.pet_id
		WHERE e.id = $1`, id).Scan(&enc.PetID, &enc.OrganizationID, &enc.Status, &guardianID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "encounter"}
	}
	if err != nil {
		return nil, err
	}
	if guardianID != nil {
		enc.GuardianID = *guardianID
	}
	return &enc, nil
}
