package infra

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra/petqueries"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries implementa app.PetQueries con sqlc sobre read_petregistry (ARQ-009).
// Toda lista filtra por guardian_id (el principal); GetPetByID solo se usa
// tras evaluar authz con el recurso cargado.
type Queries struct {
	q *petqueries.Queries
}

var _ petapp.PetQueries = (*Queries)(nil)

// NewQueries construye las consultas sobre el pool.
func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{q: petqueries.New(pool)}
}

// WithTx devuelve las consultas ligadas a la transacción (lecturas dentro del
// UoW para read-your-writes, ARQ-003).
func (r *Queries) WithTx(ctx context.Context) (*Queries, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &Queries{q: r.q.WithTx(tx)}, nil
}

// ErrPetNotFound lo devuelven GetPet y petView cuando el id no existe.
var ErrPetNotFound = &kernel.NotFoundError{Resource: "pet"}

// ListPets lista las mascotas del tutor con paginación por cursor.
func (r *Queries) ListPets(ctx context.Context, guardianID uuid.UUID, status string, cursor petapp.PetCursor, limit int) ([]petapp.PetListItem, *petapp.PetCursor, error) {
	params := petqueries.ListPetsByGuardianParams{
		GuardianID: guardianID,
	}
	if status != "" && status != "all" {
		params.Status = &status
	}
	if !cursor.CreatedAt.IsZero() {
		params.CreatedAt = pgtypeTz(cursor.CreatedAt)
		id := cursor.ID
		params.ID = &id
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	params.Limit = int32(limit + 1) //nolint:gosec // límite acotado arriba
	rows, err := r.q.ListPetsByGuardian(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	next := (*petapp.PetCursor)(nil)
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = &petapp.PetCursor{CreatedAt: last.CreatedAt.Time, ID: last.ID}
	}
	out := make([]petapp.PetListItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, petapp.PetListItem{
			ID: row.ID, Version: row.Version, Name: row.Name, Species: row.Species,
			Breed: pgStr(row.Breed), Status: row.Status, Links: int(row.ActiveLinksCount),
			CreatedAt: row.CreatedAt.Time,
			Photo:     photoRef(row.PhotoObjectKey, row.PhotoThumbKey, row.PhotoContentType),
			BirthDate: birthDate(row.BirthDate, row.BirthDateApproximate),
		})
	}
	return out, next, nil
}

// GetPet devuelve la vista completa. No existe → *kernel.NotFoundError (404,
// también cuando el recurso pertenece a otro tutor: no revelamos existencia).
func (r *Queries) GetPet(ctx context.Context, petID uuid.UUID) (petapp.PetView, error) {
	row, err := r.q.GetPetByID(ctx, petID)
	if errors.Is(err, pgx.ErrNoRows) {
		return petapp.PetView{}, ErrPetNotFound
	}
	if err != nil {
		return petapp.PetView{}, err
	}
	links, err := r.q.ListCareLinks(ctx, petID)
	if err != nil {
		return petapp.PetView{}, err
	}
	view := petapp.PetView{
		ID: row.ID, Version: row.Version, GuardianID: row.GuardianID,
		GuardianDisplayName: row.GuardianDisplayName,
		Name: row.Name, Species: row.Species, SpeciesOther: pgStr(row.SpeciesOther),
		Breed: pgStr(row.Breed), Sex: row.Sex, Neutered: row.Neutered,
		BirthDate: birthDate(row.BirthDate, row.BirthDateApproximate),
		Markings:  pgStr(row.Markings), Microchip: pgStr(row.Microchip),
		Corrections: int(row.MicrochipCorrections),
		Photo:       photoRef(row.PhotoObjectKey, row.PhotoThumbKey, row.PhotoContentType),
		Status:      row.Status, ActiveLinks: make([]petapp.CareLinkView, 0, len(links)),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
	if row.DeceasedMarkedAt.Valid {
		t := row.DeceasedMarkedAt.Time
		view.DeceasedMarkedAt = &t
	}
	if row.DeceasedAt.Valid {
		d := kernel.CivilDate(row.DeceasedAt.Time.Format("2006-01-02"))
		view.DeceasedAt = &d
	}
	for _, l := range links {
		link := petapp.CareLinkView{
			OrganizationID: l.OrganizationID, DisplayName: l.OrgDisplayName,
			Status: l.Status, OrgSuspended: l.OrgSuspended, GrantedAt: l.GrantedAt.Time,
		}
		if l.OrgCity != nil {
			link.City = *l.OrgCity
		}
		if l.RevokedAt.Valid {
			t := l.RevokedAt.Time
			link.RevokedAt = &t
		}
		view.ActiveLinks = append(view.ActiveLinks, link)
	}
	return view, nil
}

// Reservations implementa app.MicrochipReservations (DAT-005): la reserva
// ocurre en la tx del comando, antes del Append.
type Reservations struct {
	pool *db.Pool
}

var _ petapp.MicrochipReservations = (*Reservations)(nil)

// NewReservations construye el adaptador.
func NewReservations(pool *db.Pool) *Reservations { return &Reservations{pool: pool} }

// Reserve inserta la reserva. Idempotente si es de la misma mascota;
// app.ErrMicrochipTaken si pertenece a otra (sin revelar cuál, PRD-007).
func (r *Reservations) Reserve(ctx context.Context, microchip string, petID uuid.UUID) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	q := petqueries.New(tx)
	tag, err := q.ReserveMicrochip(ctx, petqueries.ReserveMicrochipParams{Microchip: microchip, PetID: petID})
	if err != nil {
		return err
	}
	if tag == 1 {
		return nil // reservado
	}
	row, err := q.GetMicrochipReservation(ctx, microchip)
	if err != nil {
		return err
	}
	if row.PetID != petID {
		return petapp.ErrMicrochipTaken
	}
	return nil // ya era suya
}

// Release borra la reserva de un chip que pertenece a la mascota dada.
func (r *Reservations) Release(ctx context.Context, microchip string, petID uuid.UUID) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return petqueries.New(tx).ReleaseMicrochip(ctx, petqueries.ReleaseMicrochipParams{Microchip: microchip, PetID: petID})
}

// GuardianAdapter adapta identity.app.Service al puerto app.GuardianLookup
// (DOM-010, in-process).
type GuardianAdapter struct {
	svc *identityapp.Service
}

var _ petapp.GuardianLookup = (*GuardianAdapter)(nil)

// NewGuardianAdapter construye el adaptador.
func NewGuardianAdapter(svc *identityapp.Service) *GuardianAdapter { return &GuardianAdapter{svc: svc} }

// Exists implementa el puerto.
func (a *GuardianAdapter) Exists(ctx context.Context, id uuid.UUID) bool {
	_, err := a.svc.AccountInfoById(ctx, id)
	return err == nil
}

// EmailOf implementa el puerto.
func (a *GuardianAdapter) EmailOf(ctx context.Context, id uuid.UUID) (string, bool) {
	info, err := a.svc.AccountInfoById(ctx, id)
	if err != nil || info.Suspended {
		return "", false
	}
	return string(info.Email), true
}

// DisplayName implementa el puerto.
func (a *GuardianAdapter) DisplayName(ctx context.Context, id uuid.UUID) string {
	info, err := a.svc.AccountInfoById(ctx, id)
	if err != nil {
		return ""
	}
	return info.DisplayName
}

// helpers --------------------------------------------------------------------

func pgStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func pgtypeTz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func birthDate(d pgtype.Date, approx *bool) *domain.BirthDate {
	if !d.Valid {
		return nil
	}
	return &domain.BirthDate{
		Date:        kernel.CivilDate(d.Time.Format("2006-01-02")),
		Approximate: approx != nil && *approx,
	}
}

func photoRef(obj, thumb, ct *string) *domain.PhotoRef {
	if obj == nil || thumb == nil || ct == nil {
		return nil
	}
	return &domain.PhotoRef{ObjectKey: *obj, ThumbKey: *thumb, ContentType: *ct}
}
