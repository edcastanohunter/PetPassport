package infra

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	partnerapp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
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
	q        *petqueries.Queries
	raw      rawQuerier            // columnas que sqlc aún no genera (alerts, M2.5)
	guardian petapp.GuardianLookup // teléfono del tutor en la lista de pacientes (nil = vacío)
}

var _ petapp.PetQueries = (*Queries)(nil)

// NewQueries construye las consultas sobre el pool.
func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{q: petqueries.New(pool), raw: pool}
}

// WithGuardian inyecta el puerto de Identity para enriquecer pacientes.
func (r *Queries) WithGuardian(g petapp.GuardianLookup) *Queries {
	r.guardian = g
	return r
}

// WithTx devuelve las consultas ligadas a la transacción (lecturas dentro del
// UoW para read-your-writes, ARQ-003).
func (r *Queries) WithTx(ctx context.Context) (*Queries, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &Queries{q: r.q.WithTx(tx), raw: tx, guardian: r.guardian}, nil
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
		Name:                row.Name, Species: row.Species, SpeciesOther: pgStr(row.SpeciesOther),
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
	if len(row.PendingTransfer) > 0 {
		var pt petapp.PendingTransferView
		if err := json.Unmarshal(row.PendingTransfer, &pt); err != nil {
			return petapp.PetView{}, err
		}
		view.PendingTransfer = &pt
	}
	if r.raw != nil {
		var alerts []byte
		if err := r.raw.QueryRow(ctx, `SELECT alerts FROM read_petregistry.pets WHERE id = $1`, petID).Scan(&alerts); err != nil {
			return petapp.PetView{}, err
		}
		if err := json.Unmarshal(alerts, &view.Alerts); err != nil {
			return petapp.PetView{}, err
		}
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

// FindByEmail implementa el puerto: cuenta activa por email.
func (a *GuardianAdapter) FindByEmail(ctx context.Context, email string) (uuid.UUID, bool) {
	info, err := a.svc.AccountInfoByEmail(ctx, email)
	if err != nil || info.Suspended {
		return uuid.Nil, false
	}
	return info.ID, true
}

// DisplayName implementa el puerto.
func (a *GuardianAdapter) DisplayName(ctx context.Context, id uuid.UUID) string {
	info, err := a.svc.AccountInfoById(ctx, id)
	if err != nil {
		return ""
	}
	return info.DisplayName
}

// Contact implementa el puerto (RN-05: el llamador ya evaluó
// pet:guardian_contact:read).
func (a *GuardianAdapter) Contact(ctx context.Context, id uuid.UUID) (petapp.GuardianContact, bool) {
	info, err := a.svc.AccountInfoById(ctx, id)
	if err != nil {
		return petapp.GuardianContact{}, false
	}
	return petapp.GuardianContact{DisplayName: info.DisplayName, Email: info.Email.String(), Phone: info.Phone}, true
}

// OrganizationAdapter adapta partnernetwork.app.Queries al puerto
// app.OrganizationLookup (DOM-010, in-process sobre el read model).
type OrganizationAdapter struct {
	q partnerapp.Queries
}

var _ petapp.OrganizationLookup = (*OrganizationAdapter)(nil)

// NewOrganizationAdapter construye el adaptador.
func NewOrganizationAdapter(q partnerapp.Queries) *OrganizationAdapter {
	return &OrganizationAdapter{q: q}
}

// ActiveClinic implementa el puerto (I5).
func (a *OrganizationAdapter) ActiveClinic(ctx context.Context, orgID uuid.UUID) (petapp.OrgSummary, error) {
	s, err := a.q.ActiveClinic(ctx, orgID)
	if errors.Is(err, partnerapp.ErrOrgNotEligible) {
		return petapp.OrgSummary{}, petapp.ErrOrganizationNotEligible
	}
	if err != nil {
		return petapp.OrgSummary{}, err
	}
	return petapp.OrgSummary{ID: s.ID, Type: s.Type, DisplayName: s.DisplayName, City: s.City, Status: s.Status}, nil
}

// ListPatients implementa la lista de pacientes de una clínica (PRD-016). El
// teléfono del tutor se resuelve por el puerto de Identity (≤ 100 filas).
func (r *Queries) ListPatients(ctx context.Context, orgID uuid.UUID, q, status string, cursor petapp.PatientCursor, limit int) ([]petapp.PatientRow, *petapp.PatientCursor, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	params := petqueries.ListPatientsParams{OrganizationID: orgID, Limit: int32(limit + 1)} //nolint:gosec // acotado arriba
	if s := strings.TrimSpace(q); s != "" {
		escaped := likeEscaper.Replace(s)
		params.Q = &escaped
	}
	if status != "" && status != "all" {
		params.Status = &status
	}
	if !cursor.LinkedAt.IsZero() {
		params.GrantedAt = pgtypeTz(cursor.LinkedAt)
		id := cursor.PetID
		params.PetID = &id
	}
	rows, err := r.q.ListPatients(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	var next *petapp.PatientCursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = &petapp.PatientCursor{LinkedAt: last.GrantedAt.Time, PetID: last.ID}
	}
	out := make([]petapp.PatientRow, 0, len(rows))
	for _, row := range rows {
		p := petapp.PatientRow{
			PetID: row.ID, Name: row.Name, Species: row.Species, Breed: pgStr(row.Breed), Status: row.Status,
			BirthDate:  birthDate(row.BirthDate, row.BirthDateApproximate),
			Photo:      photoRef(row.PhotoObjectKey, row.PhotoThumbKey, row.PhotoContentType),
			GuardianID: row.GuardianID, GuardianDisplayName: row.GuardianDisplayName,
			LinkedAt: row.GrantedAt.Time,
		}
		if r.guardian != nil {
			if c, ok := r.guardian.Contact(ctx, row.GuardianID); ok {
				p.GuardianPhone = c.Phone
			}
		}
		out = append(out, p)
	}
	return out, next, nil
}

// likeEscaper escapa los comodines de ILIKE en la búsqueda del usuario.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// TransferByTokenHash resuelve una transferencia pendiente y vigente.
func (r *Queries) TransferByTokenHash(ctx context.Context, hash []byte, now time.Time) (petapp.TransferView, error) {
	row, err := r.q.GetPendingTransferByToken(ctx, petqueries.GetPendingTransferByTokenParams{TokenHash: hash, Now: pgtypeTz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return petapp.TransferView{}, &kernel.NotFoundError{Resource: "transfer"}
	}
	if err != nil {
		return petapp.TransferView{}, err
	}
	return petapp.TransferView{PetID: row.PetID, ExpiresAt: row.ExpiresAt.Time}, nil
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

// CountByGuardians devuelve cuántas mascotas (activas o fallecidas) tiene
// cada tutor; implementa el puerto identity.PetCounter (DOM-010) para el
// panel admin. Los tutores sin mascotas no aparecen en el mapa (0).
func (r *Queries) CountByGuardians(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := r.q.CountPetsByGuardians(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		out[row.GuardianID] = int(row.PetCount)
	}
	return out, nil
}
