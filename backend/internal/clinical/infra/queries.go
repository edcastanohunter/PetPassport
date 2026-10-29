package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries implementa app.EncounterQueries sobre read_clinical.
type Queries struct {
	pool *pgxpool.Pool
}

var _ app.EncounterQueries = (*Queries)(nil)

// NewQueries construye las consultas sobre el pool.
func NewQueries(pool *pgxpool.Pool) *Queries { return &Queries{pool: pool} }

// ErrEncounterNotFound lo devuelven Get y Access.
var ErrEncounterNotFound = &kernel.NotFoundError{Resource: "encounter"}

const encounterColumns = `
	e.id, e.version, e.pet_id, p.name, p.species, e.organization_id, e.appointment_id,
	e.type, e.status, e.started_at, e.reason, e.attending_vet_id, u.display_name,
	e.vitals, e.subjective, e.objective, e.diagnoses, e.plan, e.follow_up,
	e.signature, e.addenda, e.last_activity_at, e.created_at, e.updated_at,
	e.pdf_object_key, e.pdf_version`

const encounterFrom = `
	FROM read_clinical.encounters e
	JOIN read_petregistry.pets p ON p.id = e.pet_id
	LEFT JOIN identity.user_accounts u ON u.id = e.attending_vet_id`

// Get devuelve la vista completa del encuentro.
func (q *Queries) Get(ctx context.Context, id uuid.UUID) (app.EncounterView, error) {
	row := q.pool.QueryRow(ctx, `SELECT `+encounterColumns+encounterFrom+` WHERE e.id = $1`, id)
	v, err := scanEncounter(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.EncounterView{}, ErrEncounterNotFound
		}
		return app.EncounterView{}, err
	}
	atts, err := q.Attachments(ctx, id)
	if err != nil {
		return app.EncounterView{}, err
	}
	v.Attachments = atts
	v.Abandoned = v.Status == string(domain.StatusDraft) && time.Since(v.LastActivityAt) > domain.AbandonAfter
	return v, nil
}

// Access devuelve la vista mínima de autorización.
func (q *Queries) Access(ctx context.Context, id uuid.UUID) (app.EncounterAccess, error) {
	var a app.EncounterAccess
	err := q.pool.QueryRow(ctx, `
		SELECT e.id, e.pet_id, e.organization_id, e.status, e.attending_vet_id, p.guardian_id
		FROM read_clinical.encounters e JOIN read_petregistry.pets p ON p.id = e.pet_id
		WHERE e.id = $1`, id).
		Scan(&a.ID, &a.PetID, &a.OrganizationID, &a.Status, &a.AttendingVetID, &a.GuardianID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.EncounterAccess{}, ErrEncounterNotFound
	}
	return a, err
}

// Attachments lista los adjuntos del encuentro (cuerpo y adendas).
func (q *Queries) Attachments(ctx context.Context, encounterID uuid.UUID) ([]app.AttachmentView, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, object_key, content_type, size_bytes, COALESCE(description,''), visible_to_guardian,
		       addendum_seq, created_at
		FROM read_clinical.encounter_attachments WHERE encounter_id = $1
		ORDER BY addendum_seq NULLS FIRST, created_at, id`, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.AttachmentView
	for rows.Next() {
		var a app.AttachmentView
		if err := rows.Scan(&a.ID, &a.ObjectKey, &a.ContentType, &a.SizeBytes, &a.Description,
			&a.VisibleToGuardian, &a.AddendumSeq, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanEncounter(row rowScanner) (app.EncounterView, error) {
	var v app.EncounterView
	var vitals, objective, diagnoses, plan, followUp, signature, addenda []byte
	var pdfKey *string
	var pdfVersion *int
	err := row.Scan(&v.ID, &v.Version, &v.PetID, &v.PetName, &v.PetSpecies, &v.OrganizationID,
		&v.AppointmentID, &v.Type, &v.Status, &v.StartedAt, &v.Reason, &v.AttendingVetID, &v.AttendingVetName,
		&vitals, &v.Subjective, &objective, &diagnoses, &plan, &followUp, &signature, &addenda,
		&v.LastActivityAt, &v.CreatedAt, &v.UpdatedAt, &pdfKey, &pdfVersion)
	if err != nil {
		return app.EncounterView{}, err
	}
	if len(vitals) > 0 {
		var x domain.Vitals
		if err := json.Unmarshal(vitals, &x); err != nil {
			return app.EncounterView{}, err
		}
		v.Vitals = &x
	}
	if len(objective) > 0 && string(objective) != "null" {
		var x domain.Objective
		if err := json.Unmarshal(objective, &x); err != nil {
			return app.EncounterView{}, err
		}
		v.Objective = &x
	}
	if len(diagnoses) > 0 {
		if err := json.Unmarshal(diagnoses, &v.Diagnoses); err != nil {
			return app.EncounterView{}, err
		}
	}
	if len(plan) > 0 {
		if err := json.Unmarshal(plan, &v.Plan); err != nil {
			return app.EncounterView{}, err
		}
	}
	if len(followUp) > 0 && string(followUp) != "null" {
		var x struct {
			DueDate string `json:"due_date"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(followUp, &x); err != nil {
			return app.EncounterView{}, err
		}
		v.FollowUp = &domain.FollowUpRecommendation{DueDate: kernel.CivilDate(x.DueDate), Reason: x.Reason}
	}
	if len(signature) > 0 && string(signature) != "null" {
		var x domain.Signature
		if err := json.Unmarshal(signature, &x); err != nil {
			return app.EncounterView{}, err
		}
		v.Signature = &x
	}
	if len(addenda) > 0 {
		if err := json.Unmarshal(addenda, &v.Addenda); err != nil {
			return app.EncounterView{}, err
		}
	}
	if v.Plan.Medications == nil {
		v.Plan.Medications = []domain.MedicationItem{}
	}
	if v.Plan.Procedures == nil {
		v.Plan.Procedures = []domain.Procedure{}
	}
	if v.Diagnoses == nil {
		v.Diagnoses = []domain.Diagnosis{}
	}
	if v.Addenda == nil {
		v.Addenda = []domain.Addendum{}
	}
	if v.Attachments == nil {
		v.Attachments = []app.AttachmentView{}
	}
	if pdfKey != nil {
		v.PDF = &app.PDFView{Status: "ready", ObjectKey: *pdfKey}
		if pdfVersion != nil {
			v.PDF.Version = *pdfVersion
		}
	} else if v.Signature != nil {
		v.PDF = &app.PDFView{Status: "pending"}
	}
	return v, nil
}

// ------------------------------------------------------------- listados

const listColumns = `
	e.id, e.version, e.pet_id, p.name, p.species, e.organization_id, e.type, e.status,
	e.started_at, e.reason, e.attending_vet_id, (e.signature->>'signed_at')::timestamptz,
	e.last_activity_at`

func scanList(rows pgx.Rows) ([]app.EncounterListItem, error) {
	var out []app.EncounterListItem
	for rows.Next() {
		var it app.EncounterListItem
		if err := rows.Scan(&it.ID, &it.Version, &it.PetID, &it.PetName, &it.PetSpecies,
			&it.OrganizationID, &it.Type, &it.Status, &it.StartedAt, &it.Reason,
			&it.AttendingVetID, &it.SignedAt, &it.LastActivityAt); err != nil {
			return nil, err
		}
		it.Abandoned = it.Status == string(domain.StatusDraft) && time.Since(it.LastActivityAt) > domain.AbandonAfter
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListByOrg lista consultas de una organización (API-001).
func (q *Queries) ListByOrg(ctx context.Context, f app.EncounterFilter) ([]app.EncounterListItem, *app.Cursor, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []any{f.OrganizationID}
	where := ` WHERE e.organization_id = $1`
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.PetID != nil {
		add("e.pet_id = $%d", *f.PetID)
	}
	if f.Status != "" && f.Status != "all" {
		add("e.status = $%d", f.Status)
	}
	if f.ProfessionalID != nil {
		add("e.attending_vet_id = $%d", *f.ProfessionalID)
	}
	if f.From != nil {
		add("e.started_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("e.started_at <= $%d", *f.To)
	}
	if !f.Cursor.StartedAt.IsZero() {
		args = append(args, f.Cursor.StartedAt, f.Cursor.ID)
		where += fmt.Sprintf(" AND (e.started_at, e.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	rows, err := q.pool.Query(ctx, `SELECT `+listColumns+` FROM read_clinical.encounters e
		JOIN read_petregistry.pets p ON p.id = e.pet_id`+where+
		fmt.Sprintf(" ORDER BY e.started_at DESC, e.id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items, err := scanList(rows)
	if err != nil {
		return nil, nil, err
	}
	return paginate(items, limit)
}

// ListByPet lista consultas visibles de una mascota. onlyPublished oculta los
// borradores (vista de tutor, API-001); orgIDs limita la vista de un miembro
// sin vínculo a sus propias consultas firmadas.
func (q *Queries) ListByPet(ctx context.Context, petID uuid.UUID, orgIDs []uuid.UUID, onlyPublished bool, limit int) ([]app.EncounterListItem, *app.Cursor, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := ` WHERE e.pet_id = $1`
	args := []any{petID}
	if onlyPublished {
		where += ` AND e.status = 'signed'`
	}
	if orgIDs != nil {
		args = append(args, orgIDs)
		where += fmt.Sprintf(` AND e.organization_id = ANY($%d)`, len(args))
	}
	args = append(args, limit+1)
	rows, err := q.pool.Query(ctx, `SELECT `+listColumns+` FROM read_clinical.encounters e
		JOIN read_petregistry.pets p ON p.id = e.pet_id`+where+
		fmt.Sprintf(` ORDER BY e.started_at DESC, e.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items, err := scanList(rows)
	if err != nil {
		return nil, nil, err
	}
	return paginate(items, limit)
}

func paginate(items []app.EncounterListItem, limit int) ([]app.EncounterListItem, *app.Cursor, error) {
	if len(items) <= limit {
		return items, nil, nil
	}
	items = items[:limit]
	last := items[len(items)-1]
	return items, &app.Cursor{StartedAt: last.StartedAt, ID: last.ID}, nil
}

// ------------------------------------------------------------- peso

// WeightHistory devuelve el historial de peso y la tendencia (PRD-025).
func (q *Queries) WeightHistory(ctx context.Context, petID uuid.UUID) ([]app.WeightPoint, *app.WeightTrend, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT measured_at, weight_kg::float8, source, ref_id, organization_id
		FROM read_clinical.weight_history WHERE pet_id = $1
		ORDER BY measured_at ASC, ref_id ASC`, petID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []app.WeightPoint
	for rows.Next() {
		var p app.WeightPoint
		if err := rows.Scan(&p.MeasuredAt, &p.WeightKg, &p.Source, &p.RefID, &p.OrganizationID); err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, weightTrend(out, 90), nil
}

// weightTrend compara la última medición con la más antigua de la ventana
// (o la anterior si no hay ninguna dentro). Alerta si varía > 10 %.
func weightTrend(points []app.WeightPoint, windowDays int) *app.WeightTrend {
	if len(points) < 2 {
		return nil
	}
	last := points[len(points)-1]
	cutoff := last.MeasuredAt.AddDate(0, 0, -windowDays)
	var base app.WeightPoint
	found := false
	for _, p := range points[:len(points)-1] {
		if !p.MeasuredAt.Before(cutoff) {
			base, found = p, true
			break
		}
	}
	if !found || base.WeightKg == 0 {
		return nil
	}
	delta := last.WeightKg - base.WeightKg
	pct := delta / base.WeightKg * 100
	dk, dp := math.Round(delta*100)/100, math.Round(pct*10)/10
	t := &app.WeightTrend{
		DeltaKg: &dk, DeltaPct: &dp,
		WindowDays: windowDays, Alert: math.Abs(pct) > 10,
	}
	if t.Alert {
		t.AlertReason = "WEIGHT_VARIATION_GT_10"
	}
	return t
}

// ------------------------------------------------------------- timeline

// Timeline lista la línea de tiempo visible para el actor (API-003).
func (q *Queries) Timeline(ctx context.Context, f app.TimelineFilter) ([]app.TimelineItem, *app.Cursor, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []any{f.PetID}
	where := ` WHERE pet_id = $1`
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.Kind != "" {
		add("kind = $%d", f.Kind)
	}
	if f.OrganizationID != nil {
		add("organization_id = $%d", *f.OrganizationID)
	}
	if f.From != nil {
		add("occurred_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("occurred_at <= $%d", *f.To)
	}
	if f.OrgIDs != nil {
		args = append(args, f.OrgIDs)
		where += fmt.Sprintf(" AND (visibility = 'guardian' OR organization_id = ANY($%d))", len(args))
	}
	if !f.Cursor.StartedAt.IsZero() {
		args = append(args, f.Cursor.StartedAt, f.Cursor.ID)
		where += fmt.Sprintf(" AND (occurred_at, ref_id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	rows, err := q.pool.Query(ctx, `SELECT occurred_at, kind, ref_id, organization_id, title,
		COALESCE(summary,''), position, visibility FROM read_clinical.medical_timeline`+where+
		fmt.Sprintf(` ORDER BY occurred_at DESC, ref_id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []app.TimelineItem
	for rows.Next() {
		var it app.TimelineItem
		if err := rows.Scan(&it.OccurredAt, &it.Kind, &it.RefID, &it.OrganizationID,
			&it.Title, &it.Summary, &it.Position, &it.Visibility); err != nil {
			return nil, nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	out = out[:limit]
	last := out[len(out)-1]
	return out, &app.Cursor{StartedAt: last.OccurredAt, ID: last.RefID}, nil
}

// ------------------------------------------------------------- resumen

// Summary compone la ficha de paciente (API-003).
func (q *Queries) Summary(ctx context.Context, petID uuid.UUID) (app.SummaryView, error) {
	var out app.SummaryView
	rows, err := q.pool.Query(ctx, `
		SELECT e.id, e.version, e.pet_id, p.name, p.species, e.organization_id, e.type, e.status,
		       e.started_at, e.reason, e.attending_vet_id, (e.signature->>'signed_at')::timestamptz,
		       e.last_activity_at
		FROM read_clinical.encounters e JOIN read_petregistry.pets p ON p.id = e.pet_id
		WHERE e.pet_id = $1 AND e.status = 'signed'
		ORDER BY e.started_at DESC, e.id DESC LIMIT 1`, petID)
	if err != nil {
		return app.SummaryView{}, err
	}
	items, err := scanList(rows)
	rows.Close()
	if err != nil {
		return app.SummaryView{}, err
	}
	if len(items) > 0 {
		out.LastEncounter = &items[0]
	}
	frows, err := q.pool.Query(ctx, `
		SELECT id, organization_id, pet_id, follow_up->>'due_date', follow_up->>'reason'
		FROM read_clinical.encounters
		WHERE pet_id = $1 AND status = 'signed' AND follow_up IS NOT NULL
		ORDER BY (follow_up->>'due_date') ASC`, petID)
	if err != nil {
		return app.SummaryView{}, err
	}
	defer frows.Close()
	for frows.Next() {
		var f app.FollowUpItem
		if err := frows.Scan(&f.EncounterID, &f.OrganizationID, &f.PetID, &f.DueDate, &f.Reason); err != nil {
			return app.SummaryView{}, err
		}
		out.OpenFollowUps = append(out.OpenFollowUps, f)
		if len(out.NextDue) < 3 {
			out.NextDue = append(out.NextDue, f)
		}
	}
	if err := frows.Err(); err != nil {
		return app.SummaryView{}, err
	}
	var weight app.WeightPoint
	err = q.pool.QueryRow(ctx, `
		SELECT measured_at, weight_kg::float8, source, ref_id, organization_id
		FROM read_clinical.weight_history WHERE pet_id = $1
		ORDER BY measured_at DESC, ref_id DESC LIMIT 1`, petID).
		Scan(&weight.MeasuredAt, &weight.WeightKg, &weight.Source, &weight.RefID, &weight.OrganizationID)
	if err == nil {
		out.CurrentWeight = &weight
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return app.SummaryView{}, err
	}
	out.Alerts = []string{}
	if out.NextDue == nil {
		out.NextDue = []app.FollowUpItem{}
	}
	if out.OpenFollowUps == nil {
		out.OpenFollowUps = []app.FollowUpItem{}
	}
	return out, nil
}
