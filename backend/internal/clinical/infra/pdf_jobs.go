package infra

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"rsc.io/qr"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
)

//go:embed templates/encounter.typ
var encounterTemplate []byte

// Parámetros del runner de documentos (INF-004).
const (
	pdfMaxAttempts  = 3
	pdfStaleAfter   = 2 * time.Minute // trabajo 'rendering' huérfano (worker caído)
	pdfPollInterval = 2 * time.Second
)

// Renderer es el puerto del motor de PDF (pdfrender.Typst).
type Renderer interface {
	Render(ctx context.Context, template, data []byte, files ...pdfrender.File) ([]byte, error)
}

// PDFWorker procesa read_clinical.document_jobs de tipo encounter_pdf: toma
// un trabajo (SKIP LOCKED), compone los datos desde el read model, renderiza
// con Typst y sube `clinical/<pet>/<encounter>/v<k>.pdf`. El trabajo es la
// caché por (kind, ref_id, version): una versión lista no se re-renderiza.
type PDFWorker struct {
	pool      *pgxpool.Pool
	queries   *Queries
	objects   storage.ObjectStorage
	renderer  Renderer
	verifyURL string // base pública: <base>/verify/encounters/<id>?hash=<h>
	logger    *slog.Logger
	duration  *prometheus.HistogramVec
	workers   int
}

// NewPDFWorker construye el runner. reg puede ser nil (pruebas).
func NewPDFWorker(pool *pgxpool.Pool, objects storage.ObjectStorage, renderer Renderer, publicBaseURL string, reg prometheus.Registerer, logger *slog.Logger) *PDFWorker {
	if logger == nil {
		logger = slog.Default()
	}
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "document_render_duration_seconds",
		Help:    "Duración del renderizado de documentos PDF (INF-004).",
		Buckets: []float64{0.25, 0.5, 1, 2, 5, 10, 20, 30},
	}, []string{"kind", "outcome"})
	if reg != nil {
		reg.MustRegister(h)
	}
	return &PDFWorker{
		pool: pool, queries: NewQueries(pool), objects: objects, renderer: renderer,
		verifyURL: strings.TrimRight(publicBaseURL, "/") + "/verify/encounters/",
		logger:    logger, duration: h, workers: pdfrender.DefaultConcurrency,
	}
}

// Run procesa trabajos hasta que el contexto se cancele (concurrencia 2).
func (w *PDFWorker) Run(ctx context.Context) error {
	errs := make(chan error, w.workers)
	for range w.workers {
		go func() { errs <- w.loop(ctx) }()
	}
	var first error
	for range w.workers {
		if err := <-errs; err != nil && first == nil && !errors.Is(err, context.Canceled) {
			first = err
		}
	}
	return first
}

func (w *PDFWorker) loop(ctx context.Context) error {
	t := time.NewTicker(pdfPollInterval)
	defer t.Stop()
	for {
		for {
			done, err := w.ProcessOne(ctx)
			if err != nil {
				w.logger.ErrorContext(ctx, "pdf: error del runner", "error", err)
				break
			}
			if !done {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// pdfJob es un trabajo reclamado.
type pdfJob struct {
	id       uuid.UUID
	kind     string
	ref      uuid.UUID
	version  int
	attempts int
}

// ProcessOne reclama y procesa un trabajo; false si no había ninguno.
func (w *PDFWorker) ProcessOne(ctx context.Context) (bool, error) {
	var j pdfJob
	err := w.pool.QueryRow(ctx, `
		UPDATE read_clinical.document_jobs SET status = 'rendering', attempts = attempts + 1, updated_at = now()
		WHERE id = (
			SELECT id FROM read_clinical.document_jobs
			WHERE kind = ANY($1) AND (status = 'pending' OR (status = 'rendering' AND updated_at < now() - make_interval(secs => $2)))
			ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, kind, ref_id, version, attempts`,
		[]string{app.DocumentJobKindEncounterPDF, app.DocumentJobKindCardPDF}, pdfStaleAfter.Seconds()).
		Scan(&j.id, &j.kind, &j.ref, &j.version, &j.attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	start := time.Now()
	key, err := w.render(ctx, j)
	outcome := "ready"
	if err != nil {
		outcome = "failed"
		// Sin PII en el error persistido ni en logs (solo ids y causa técnica).
		w.logger.WarnContext(ctx, "pdf: render fallido", "job_id", j.id, "kind", j.kind, "ref_id", j.ref, "attempt", j.attempts, "error", err)
		status := "pending"
		if j.attempts >= pdfMaxAttempts {
			status = "failed"
		}
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		_, uerr := w.pool.Exec(ctx, `UPDATE read_clinical.document_jobs
			SET status = $2, last_error = $3, updated_at = now() WHERE id = $1`, j.id, status, msg)
		w.duration.WithLabelValues(j.kind, outcome).Observe(time.Since(start).Seconds())
		return true, uerr
	}
	_, err = w.pool.Exec(ctx, `UPDATE read_clinical.document_jobs
		SET status = 'ready', object_key = $2, last_error = NULL, updated_at = now() WHERE id = $1`, j.id, key)
	w.duration.WithLabelValues(j.kind, outcome).Observe(time.Since(start).Seconds())
	return true, err
}

// PDFKey es la clave del objeto de la versión k (ARQ-010).
func PDFKey(pet, enc uuid.UUID, version int) string {
	return "clinical/" + pet.String() + "/" + enc.String() + "/v" + strconv.Itoa(version) + ".pdf"
}

func (w *PDFWorker) render(ctx context.Context, j pdfJob) (string, error) {
	if j.kind == app.DocumentJobKindCardPDF {
		return w.renderCard(ctx, j)
	}
	v, err := w.queries.Get(ctx, j.ref)
	if err != nil {
		return "", err
	}
	if v.Signature == nil {
		return "", errors.New("encuentro sin firmar")
	}
	data, err := w.documentData(ctx, v, j.version)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	code, err := qr.Encode(data.VerifyURL, qr.M)
	if err != nil {
		return "", fmt.Errorf("qr: %w", err)
	}
	pdf, err := w.renderer.Render(ctx, encounterTemplate, raw, pdfrender.File{Name: "qr.png", Data: code.PNG()})
	if err != nil {
		return "", err
	}
	key := PDFKey(v.PetID, v.ID, j.version)
	if err := w.objects.Put(ctx, key, "application/pdf", bytes.NewReader(pdf), int64(len(pdf))); err != nil {
		return "", err
	}
	return key, nil
}

// ------------------------------------------------------------- datos

// EncounterDocument son los datos de la plantilla (data.json).
type EncounterDocument struct {
	EncounterID      string        `json:"encounter_id"`
	DocumentVersion  int           `json:"document_version"`
	OrganizationName string        `json:"organization_name"`
	TypeLabel        string        `json:"type_label"`
	PetName          string        `json:"pet_name"`
	PetSpecies       string        `json:"pet_species"`
	StartedAt        string        `json:"started_at"`
	AttendingVet     string        `json:"attending_vet"`
	Reason           string        `json:"reason"`
	Vitals           []docPair     `json:"vitals"`
	Subjective       string        `json:"subjective"`
	Objective        string        `json:"objective"`
	Findings         []docFinding  `json:"findings"`
	Diagnoses        []docDiag     `json:"diagnoses"`
	Medications      []docMed      `json:"medications"`
	Procedures       []docProc     `json:"procedures"`
	Recommendations  string        `json:"recommendations"`
	FollowUp         string        `json:"follow_up"`
	Attachments      []string      `json:"attachments"`
	SignedBy         string        `json:"signed_by"`
	SignedAt         string        `json:"signed_at"`
	StreamVersion    int64         `json:"stream_version"`
	ContentHash      string        `json:"content_hash"`
	VerifyURL        string        `json:"verify_url"`
	Addenda          []docAddendum `json:"addenda"`
}

type docPair struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type docFinding struct {
	System string `json:"system"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type docDiag struct {
	Text      string `json:"text"`
	Certainty string `json:"certainty"`
	Code      string `json:"code"`
}

type docMed struct {
	Name      string `json:"name"`
	Dose      string `json:"dose"`
	Route     string `json:"route"`
	Frequency string `json:"frequency"`
	Duration  string `json:"duration"`
}

type docProc struct {
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

type docAddendum struct {
	Seq      int    `json:"seq"`
	Reason   string `json:"reason"`
	Text     string `json:"text"`
	SignedBy string `json:"signed_by"`
	SignedAt string `json:"signed_at"`
}

var typeLabels = map[string]string{
	"consultation": "Consulta", "follow_up": "Control", "emergency": "Urgencia", "vaccination": "Vacunación",
	"surgery": "Cirugía", "procedure": "Procedimiento", "other": "Otro",
}

var addendumLabels = map[string]string{
	"correction": "Corrección", "additional_findings": "Hallazgos adicionales", "lab_results": "Resultados de laboratorio", "other": "Otro",
}

var certaintyLabels = map[string]string{
	"presumptive": "presuntivo", "confirmed": "confirmado", "differential": "diferencial", "ruled_out": "descartado",
}

var findingLabels = map[string]string{"normal": "Normal", "abnormal": "Anormal", "not_examined": "No examinado"}

func fmtTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// documentData compone data.json; los nombres de firmantes se resuelven en
// identity.user_accounts.
func (w *PDFWorker) documentData(ctx context.Context, v app.EncounterView, version int) (EncounterDocument, error) {
	names, err := w.displayNames(ctx, v)
	if err != nil {
		return EncounterDocument{}, err
	}
	var orgName string
	if err := w.pool.QueryRow(ctx, `SELECT display_name FROM read_partnernetwork.organizations WHERE id = $1`,
		v.OrganizationID).Scan(&orgName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EncounterDocument{}, err
	}
	return BuildEncounterDocument(v, version, orgName, names, w.verifyURL), nil
}

func (w *PDFWorker) displayNames(ctx context.Context, v app.EncounterView) (map[uuid.UUID]string, error) {
	ids := []uuid.UUID{v.Signature.SignedBy}
	for _, a := range v.Addenda {
		ids = append(ids, a.SignedBy)
	}
	rows, err := w.pool.Query(ctx, `SELECT id, display_name FROM identity.user_accounts WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// BuildEncounterDocument es puro (golden test de la plantilla).
func BuildEncounterDocument(v app.EncounterView, version int, orgName string, names map[uuid.UUID]string, verifyBase string) EncounterDocument {
	d := EncounterDocument{
		EncounterID: v.ID.String(), DocumentVersion: version, OrganizationName: orgName,
		TypeLabel: typeLabels[v.Type], PetName: v.PetName, PetSpecies: v.PetSpecies,
		StartedAt: fmtTime(v.StartedAt), AttendingVet: v.AttendingVetName, Reason: v.Reason,
		Vitals: []docPair{}, Findings: []docFinding{}, Diagnoses: []docDiag{}, Medications: []docMed{},
		Procedures: []docProc{}, Attachments: []string{}, Addenda: []docAddendum{},
		Recommendations: string(v.Plan.Recommendations),
	}
	if d.TypeLabel == "" {
		d.TypeLabel = v.Type
	}
	if v.Subjective != nil {
		d.Subjective = *v.Subjective
	}
	if vt := v.Vitals; vt != nil {
		add := func(label string, val string) { d.Vitals = append(d.Vitals, docPair{label, val}) }
		if vt.WeightKg != nil {
			add("Peso", fmtNum(*vt.WeightKg)+" kg")
		}
		if vt.TempC != nil {
			add("Temperatura", fmtNum(*vt.TempC)+" °C")
		}
		if vt.HeartRateBpm != nil {
			add("Frecuencia cardiaca", strconv.Itoa(*vt.HeartRateBpm)+" lpm")
		}
		if vt.RespRateRpm != nil {
			add("Frecuencia respiratoria", strconv.Itoa(*vt.RespRateRpm)+" rpm")
		}
		if vt.BodyCondition != nil {
			add("Condición corporal", strconv.Itoa(*vt.BodyCondition)+" / 9")
		}
		if vt.Mucosa != nil {
			add("Mucosas", string(*vt.Mucosa))
		}
		if vt.CRTSeconds != nil {
			add("TRC", fmtNum(*vt.CRTSeconds)+" s")
		}
		if vt.HydrationPct != nil {
			add("Deshidratación", fmtNum(*vt.HydrationPct)+" %")
		}
	}
	if o := v.Objective; o != nil {
		d.Objective = string(o.Narrative)
		systems := make([]string, 0, len(o.Systems))
		for k := range o.Systems {
			systems = append(systems, string(k))
		}
		sort.Strings(systems)
		for _, s := range systems {
			f := o.Systems[domain.BodySystem(s)]
			d.Findings = append(d.Findings, docFinding{System: s, Status: findingLabels[string(f.Status)], Note: f.Note})
		}
	}
	for _, x := range v.Diagnoses {
		dg := docDiag{Text: x.Text, Certainty: certaintyLabels[string(x.Certainty)]}
		if dg.Certainty == "" {
			dg.Certainty = string(x.Certainty)
		}
		if x.Code != nil {
			dg.Code = x.Code.System + " " + x.Code.Value
		}
		d.Diagnoses = append(d.Diagnoses, dg)
	}
	for _, m := range v.Plan.Medications {
		freq := "dosis única"
		if !m.Frequency.Once {
			unit := "h"
			if m.Frequency.Unit == "days" {
				unit = "días"
			}
			freq = "cada " + strconv.Itoa(m.Frequency.Every) + " " + unit
		}
		dur := ""
		if m.DurationDays != nil {
			dur = strconv.Itoa(*m.DurationDays) + " días"
		}
		name := m.MedicationName
		if m.Presentation != "" {
			name += " (" + m.Presentation + ")"
		}
		d.Medications = append(d.Medications, docMed{Name: name, Dose: fmtNum(m.Dose) + " " + string(m.DoseUnit),
			Route: string(m.Route), Frequency: freq, Duration: dur})
	}
	for _, p := range v.Plan.Procedures {
		d.Procedures = append(d.Procedures, docProc{Name: p.Name, Notes: p.Notes})
	}
	if v.FollowUp != nil {
		d.FollowUp = string(v.FollowUp.DueDate) + " — " + v.FollowUp.Reason
	}
	for _, a := range v.Attachments {
		if a.AddendumSeq != nil && version > 0 && *a.AddendumSeq >= version {
			continue
		}
		label := strings.ToUpper(a.ContentType)
		if a.Description != "" {
			label += ": " + a.Description
		}
		if a.AddendumSeq != nil {
			label += " (adenda " + strconv.Itoa(*a.AddendumSeq) + ")"
		}
		d.Attachments = append(d.Attachments, label)
	}
	sig := v.Signature
	d.SignedBy = names[sig.SignedBy]
	d.SignedAt = fmtTime(sig.SignedAt)
	d.StreamVersion = sig.StreamVersion
	d.ContentHash = sig.ContentHash
	d.VerifyURL = verifyBase + v.ID.String() + "?hash=" + sig.ContentHash
	for _, a := range v.Addenda {
		if version > 0 && a.Seq >= version {
			continue // la versión k incluye las adendas 1..k-1
		}
		reason := addendumLabels[string(a.Reason)]
		if reason == "" {
			reason = string(a.Reason)
		}
		d.Addenda = append(d.Addenda, docAddendum{Seq: a.Seq, Reason: reason, Text: a.Text,
			SignedBy: names[a.SignedBy], SignedAt: fmtTime(a.SignedAt)})
	}
	return d
}
