package infra

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CardQueries implementa app.CardQueries y app.Catalog sobre read_clinical y ref.
type CardQueries struct{ pool *pgxpool.Pool }

var (
	_ app.CardQueries = (*CardQueries)(nil)
	_ app.Catalog     = (*CardQueries)(nil)
)

// NewCardQueries construye las consultas del carnet.
func NewCardQueries(pool *pgxpool.Pool) *CardQueries { return &CardQueries{pool: pool} }

const entryColumns = `id, kind, catalog_id, product_name, to_char(administered_at,'YYYY-MM-DD'), lot, manufacturer,
	to_char(expiry_date,'YYYY-MM-DD'), route, site, dose_label, weight_kg::float8, source, verification,
	recorded_by_org, recorded_by_user, declared_by, encounter_id, to_char(next_due_date,'YYYY-MM-DD'), next_due_basis,
	status, void_reason, evidence_object_key, recorded_at, created_at`

func scanEntry(row pgx.Row) (app.CardEntryView, error) {
	var v app.CardEntryView
	err := row.Scan(&v.ID, &v.Kind, &v.CatalogID, &v.ProductName, &v.AdministeredAt, &v.Lot, &v.Manufacturer,
		&v.ExpiryDate, &v.Route, &v.Site, &v.DoseLabel, &v.WeightKg, &v.Source, &v.Verification,
		&v.RecordedByOrg, &v.RecordedByUser, &v.DeclaredBy, &v.EncounterID, &v.NextDueDate, &v.NextDueBasis,
		&v.Status, &v.VoidReason, &v.EvidenceKey, &v.RecordedAt, &v.CreatedAt)
	v.HasEvidence = v.EvidenceKey != nil
	return v, err
}

// Card devuelve el carnet; con includeVoided las anuladas salen tachadas (API-004).
func (q *CardQueries) Card(ctx context.Context, petID uuid.UUID, includeVoided bool) (app.CardView, error) {
	view := app.CardView{PetID: petID, Entries: []app.CardEntryView{}, NextDue: []app.NextDueItem{}}
	err := q.pool.QueryRow(ctx, `SELECT species FROM read_clinical.cards WHERE pet_id = $1`, petID).Scan(&view.Species)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return view, err
	}
	rows, err := q.pool.Query(ctx, `SELECT `+entryColumns+` FROM read_clinical.card_entries
		WHERE pet_id = $1 AND ($2 OR status = 'active')
		ORDER BY administered_at DESC, id`, petID, includeVoided)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanEntry(rows)
		if err != nil {
			return view, err
		}
		view.Entries = append(view.Entries, v)
	}
	if err := rows.Err(); err != nil {
		return view, err
	}
	// Próximas dosis desde las entradas (síncrono: sin retraso). El estado
	// "overdue" se deriva de la fecha; vaccination_due alimenta recordatorios.
	for _, e := range view.Entries {
		if e.Status != "active" || e.NextDueDate == nil {
			continue
		}
		view.NextDue = append(view.NextDue, app.NextDueItem{
			EntryID: e.ID, Kind: e.Kind, ProductName: e.ProductName, DueDate: *e.NextDueDate, Basis: deref(e.NextDueBasis),
		})
	}
	// Una próxima dosis ya cumplida (dosis posterior del mismo producto) sale como satisfied.
	for i, n := range view.NextDue {
		view.NextDue[i].Status = "upcoming"
		for _, e := range view.Entries {
			if e.Status == "active" && e.Kind == n.Kind && strings.EqualFold(e.ProductName, n.ProductName) &&
				e.AdministeredAt > entryDate(view.Entries, n.EntryID) {
				view.NextDue[i].Status = "satisfied"
				break
			}
		}
		if view.NextDue[i].Status == "upcoming" && n.DueDate < time.Now().UTC().Format("2006-01-02") {
			view.NextDue[i].Status = "overdue"
		}
	}
	return view, nil
}

func entryDate(es []app.CardEntryView, id uuid.UUID) string {
	for _, e := range es {
		if e.ID == id {
			return e.AdministeredAt
		}
	}
	return ""
}

// Entry devuelve una entrada de la mascota (404 si no existe).
func (q *CardQueries) Entry(ctx context.Context, petID, entryID uuid.UUID) (app.CardEntryView, error) {
	v, err := scanEntry(q.pool.QueryRow(ctx, `SELECT `+entryColumns+` FROM read_clinical.card_entries
		WHERE id = $1 AND pet_id = $2`, entryID, petID))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &kernel.NotFoundError{Resource: "card_entry"}
	}
	return v, err
}

// ------------------------------------------------------------ catálogos

// Vaccine devuelve una vacuna del catálogo (404 si no existe).
func (q *CardQueries) Vaccine(ctx context.Context, id uuid.UUID) (app.VaccineRef, error) {
	var v app.VaccineRef
	err := q.pool.QueryRow(ctx, `SELECT id, name, species, diseases, type, routes, active FROM ref.vaccines WHERE id = $1`, id).
		Scan(&v.ID, &v.Name, &v.Species, &v.Diseases, &v.Type, &v.Routes, &v.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &kernel.NotFoundError{Resource: "vaccine"}
	}
	return v, err
}

// Vaccines lista las vacunas activas filtrando por especie y texto.
func (q *CardQueries) Vaccines(ctx context.Context, species, search string) ([]app.VaccineRef, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, name, species, diseases, type, routes, active FROM ref.vaccines
		WHERE active AND ($1 = '' OR $1 = ANY(species)) AND ($2 = '' OR name ILIKE '%' || $2 || '%')
		ORDER BY name`, species, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.VaccineRef{}
	for rows.Next() {
		var v app.VaccineRef
		if err := rows.Scan(&v.ID, &v.Name, &v.Species, &v.Diseases, &v.Type, &v.Routes, &v.Active); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanProtocol(row pgx.Row) (app.ProtocolRef, error) {
	var p app.ProtocolRef
	var series []byte
	var notes *string
	err := row.Scan(&p.ID, &p.Country, &p.Species, &p.VaccineID, &series, &p.BoosterIntervalMonths, &p.LegalValidityMonths, &notes, &p.Active)
	if err != nil {
		return p, err
	}
	p.Notes = deref(notes)
	if err := json.Unmarshal(series, &p.InitialSeries); err != nil {
		return p, err
	}
	if p.InitialSeries == nil {
		p.InitialSeries = []domain.SeriesDose{}
	}
	return p, nil
}

const protocolColumns = `id, country, species, vaccine_id, initial_series, booster_interval_months, legal_validity_months, notes, active`

// Protocol devuelve el protocolo activo, o nil si no hay.
func (q *CardQueries) Protocol(ctx context.Context, country, species string, vaccineID uuid.UUID) (*app.ProtocolRef, error) {
	p, err := scanProtocol(q.pool.QueryRow(ctx, `SELECT `+protocolColumns+` FROM ref.vaccination_protocols
		WHERE country = $1 AND species = $2 AND vaccine_id = $3 AND active`, country, species, vaccineID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Protocols lista los protocolos activos.
func (q *CardQueries) Protocols(ctx context.Context, country, species string) ([]app.ProtocolRef, error) {
	rows, err := q.pool.Query(ctx, `SELECT `+protocolColumns+` FROM ref.vaccination_protocols
		WHERE active AND ($1 = '' OR country = $1) AND ($2 = '' OR species = $2) ORDER BY species, vaccine_id`, country, species)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.ProtocolRef{}
	for rows.Next() {
		p, err := scanProtocol(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PublicPet implementa app.PublicPets: datos públicos para enlaces compartidos
// (SEC-002: microchip enmascarado a los últimos 4, sin contacto del tutor).
func (q *CardQueries) PublicPet(ctx context.Context, petID uuid.UUID) (app.PublicPet, error) {
	var p app.PublicPet
	err := q.pool.QueryRow(ctx, `
		SELECT name, species, breed, sex, to_char(birth_date,'YYYY-MM-DD'),
		       CASE WHEN microchip IS NULL THEN NULL ELSE repeat('*', greatest(length(microchip)-4,0)) || right(microchip,4) END,
		       photo_object_key IS NOT NULL
		  FROM read_petregistry.pets WHERE id = $1`, petID).
		Scan(&p.Name, &p.Species, &p.Breed, &p.Sex, &p.BirthDate, &p.MicrochipMasked, &p.HasPhoto)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, &kernel.NotFoundError{Resource: "pet"}
	}
	return p, err
}

var _ app.PublicPets = (*CardQueries)(nil)
