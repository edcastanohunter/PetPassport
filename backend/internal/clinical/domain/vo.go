// Package domain contiene el agregado Encounter, sus value objects, comandos
// y eventos v1 (PP-F2-DOM-003/004/005, docs/phase-2/02 §3.1–3.3). Puro: solo
// stdlib, kernel y uuid (ADR-001 regla 1).
package domain

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Límites del agregado (02 §3.1–3.2).
const (
	MaxReasonLen          = 300
	MaxNarrativeLen       = 5000
	MaxFindingNoteLen     = 500
	MaxDiagnoses          = 20
	MaxDiagnosisTextLen   = 200
	MaxMedications        = 30
	MaxProcedures         = 20
	MaxAttachments        = 10
	MaxAddenda            = 20
	MaxAddendumAttach     = 5
	MaxAttachmentBytes    = 10 << 20
	MaxDescriptionLen     = 200
	MaxFollowUpReasonLen  = 200
	MaxFollowUpDays       = 365
	MaxMedicationNameLen  = 120
	MaxInstructionsLen    = 300
	MaxProcedureNotesLen  = 1000
	MaxVisibilityReasonLn = 200
)

// EncounterType es el tipo de atención (PRD-001).
type EncounterType string

// Tipos admitidos.
const (
	TypeConsultation EncounterType = "consultation"
	TypeFollowUp     EncounterType = "follow_up"
	TypeEmergency    EncounterType = "emergency"
	TypeVaccination  EncounterType = "vaccination"
	TypeSurgery      EncounterType = "surgery"
	TypeProcedure    EncounterType = "procedure"
	TypeOther        EncounterType = "other"
)

// NewEncounterType valida el tipo.
func NewEncounterType(s string) (EncounterType, error) {
	switch v := EncounterType(strings.TrimSpace(s)); v {
	case TypeConsultation, TypeFollowUp, TypeEmergency, TypeVaccination, TypeSurgery, TypeProcedure, TypeOther:
		return v, nil
	}
	return "", errInvalid
}

// EncounterStatus es el estado del encuentro.
type EncounterStatus string

// Estados (02 §3.1).
const (
	StatusDraft     EncounterStatus = "draft"
	StatusSigned    EncounterStatus = "signed"
	StatusDiscarded EncounterStatus = "discarded"
)

// errInvalid es el error interno de los constructores de VO; los comandos lo
// traducen a kernel.ValidationError con el campo.
var errInvalid = kernel.NewDomainError("INVALID", "valor inválido")

// htmlTagRe detecta etiquetas HTML ("<b", "</", "<!"): el texto clínico se
// muestra como Markdown restringido (negrita, listas) y nunca como HTML. Un
// "<" seguido de espacio o dígito ("T < 39") es texto legítimo.
var htmlTagRe = regexp.MustCompile(`<[A-Za-z/!?]`)

// cleanText valida texto libre: sin caracteres de control (salvo saltos de
// línea y tabuladores), sin etiquetas HTML y con longitud máxima en runas.
// Devuelve el texto recortado.
func cleanText(s string, maxLen int, multiline bool) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxLen || htmlTagRe.MatchString(s) {
		return "", errInvalid
	}
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t')) {
			return "", errInvalid
		}
	}
	return s, nil
}

// Reason es el motivo de consulta (≤ 300, obligatorio).
type Reason string

// NewReason valida el motivo.
func NewReason(s string) (Reason, error) {
	v, err := cleanText(s, MaxReasonLen, false)
	if err != nil || v == "" {
		return "", errInvalid
	}
	return Reason(v), nil
}

// Narrative es texto clínico libre (≤ 5.000, Markdown restringido). Vacío
// es válido y significa "sin contenido".
type Narrative string

// NewNarrative valida el texto.
func NewNarrative(s string) (Narrative, error) {
	v, err := cleanText(s, MaxNarrativeLen, true)
	if err != nil {
		return "", err
	}
	return Narrative(v), nil
}

// Mucosa es el color de mucosas.
type Mucosa string

// Valores de mucosas.
const (
	MucosaPink      Mucosa = "pink"
	MucosaPale      Mucosa = "pale"
	MucosaWhite     Mucosa = "white"
	MucosaCyanotic  Mucosa = "cyanotic"
	MucosaIcteric   Mucosa = "icteric"
	MucosaHyperemic Mucosa = "hyperemic"
)

func validMucosa(m Mucosa) bool {
	switch m {
	case MucosaPink, MucosaPale, MucosaWhite, MucosaCyanotic, MucosaIcteric, MucosaHyperemic:
		return true
	}
	return false
}

// Vitals son las constantes vitales (PRD-002). Todos opcionales; los rangos
// de aquí son invariantes de formato; los de especie son avisos (warnings.go).
type Vitals struct {
	WeightKg      *float64 `json:"weight_kg,omitempty"`
	TempC         *float64 `json:"temp_c,omitempty"`
	HeartRateBpm  *int     `json:"heart_rate_bpm,omitempty"`
	RespRateRpm   *int     `json:"resp_rate_rpm,omitempty"`
	BodyCondition *int     `json:"body_condition,omitempty"`
	Mucosa        *Mucosa  `json:"mucosa,omitempty"`
	CRTSeconds    *float64 `json:"crt_seconds,omitempty"`
	HydrationPct  *float64 `json:"hydration_pct,omitempty"`
}

// IsEmpty informa si no hay ninguna constante.
func (v Vitals) IsEmpty() bool {
	return v.WeightKg == nil && v.TempC == nil && v.HeartRateBpm == nil && v.RespRateRpm == nil &&
		v.BodyCondition == nil && v.Mucosa == nil && v.CRTSeconds == nil && v.HydrationPct == nil
}

func inRangeF(p *float64, lo, hi float64) bool {
	return p == nil || (!math.IsNaN(*p) && *p >= lo && *p <= hi)
}

func inRangeI(p *int, lo, hi int) bool { return p == nil || (*p >= lo && *p <= hi) }

// hasMaxDecimals comprueba que x tiene como mucho d decimales.
func hasMaxDecimals(x float64, d int) bool {
	f := math.Pow10(d)
	return math.Abs(math.Round(x*f)-x*f) < 1e-6
}

// Validate comprueba los rangos de formato de DOM-004 y añade violaciones
// con prefijo de campo (p. ej. "vitals.weight_kg").
func (v Vitals) Validate(ve *kernel.ValidationError, prefix string) {
	if !inRangeF(v.WeightKg, 0.01, 500) || (v.WeightKg != nil && !hasMaxDecimals(*v.WeightKg, 2)) {
		ve.Add(prefix+"weight_kg", "OUT_OF_RANGE")
	}
	if !inRangeF(v.TempC, 30, 45) || (v.TempC != nil && !hasMaxDecimals(*v.TempC, 1)) {
		ve.Add(prefix+"temp_c", "OUT_OF_RANGE")
	}
	if !inRangeI(v.HeartRateBpm, 20, 400) {
		ve.Add(prefix+"heart_rate_bpm", "OUT_OF_RANGE")
	}
	if !inRangeI(v.RespRateRpm, 5, 200) {
		ve.Add(prefix+"resp_rate_rpm", "OUT_OF_RANGE")
	}
	if !inRangeI(v.BodyCondition, 1, 9) {
		ve.Add(prefix+"body_condition", "OUT_OF_RANGE")
	}
	if v.Mucosa != nil && !validMucosa(*v.Mucosa) {
		ve.Add(prefix+"mucosa", "INVALID")
	}
	if !inRangeF(v.CRTSeconds, 0.5, 5) {
		ve.Add(prefix+"crt_seconds", "OUT_OF_RANGE")
	}
	if !inRangeF(v.HydrationPct, 0, 15) {
		ve.Add(prefix+"hydration_pct", "OUT_OF_RANGE")
	}
}

// BodySystem es un sistema del examen físico.
type BodySystem string

// Sistemas (DOM-004).
var bodySystems = map[BodySystem]bool{
	"integumentary": true, "musculoskeletal": true, "cardiovascular": true, "respiratory": true,
	"digestive": true, "genitourinary": true, "nervous": true, "eyes": true, "ears": true,
	"lymph": true, "oral": true,
}

// FindingStatus es el resultado del examen de un sistema.
type FindingStatus string

// Resultados admitidos.
const (
	FindingNormal      FindingStatus = "normal"
	FindingAbnormal    FindingStatus = "abnormal"
	FindingNotExamined FindingStatus = "not_examined"
)

// Finding es el hallazgo de un sistema.
type Finding struct {
	Status FindingStatus `json:"status"`
	Note   string        `json:"note,omitempty"`
}

// Objective es la parte O del SOAP: narrativa + examen por sistemas.
type Objective struct {
	Narrative Narrative              `json:"narrative,omitempty"`
	Systems   map[BodySystem]Finding `json:"systems,omitempty"`
}

// IsEmpty informa si no hay hallazgos ni narrativa (E6).
func (o *Objective) IsEmpty() bool {
	return o == nil || (o.Narrative == "" && len(o.Systems) == 0)
}

// normalize valida y recorta el objetivo.
func (o Objective) normalize(ve *kernel.ValidationError) Objective {
	out := Objective{}
	n, err := NewNarrative(string(o.Narrative))
	if err != nil {
		ve.Add("objective.narrative", "INVALID")
	}
	out.Narrative = n
	if len(o.Systems) > 0 {
		out.Systems = make(map[BodySystem]Finding, len(o.Systems))
	}
	for sys, f := range o.Systems {
		field := "objective.systems." + string(sys)
		if !bodySystems[sys] {
			ve.Add(field, "INVALID")
			continue
		}
		switch f.Status {
		case FindingNormal, FindingAbnormal, FindingNotExamined:
		default:
			ve.Add(field+".status", "INVALID")
		}
		note, err := cleanText(f.Note, MaxFindingNoteLen, true)
		if err != nil {
			ve.Add(field+".note", "INVALID")
		}
		out.Systems[sys] = Finding{Status: f.Status, Note: note}
	}
	return out
}

// Certainty es la certeza de un diagnóstico.
type Certainty string

// Certezas (PRD-003).
const (
	CertaintyPresumptive  Certainty = "presumptive"
	CertaintyConfirmed    Certainty = "confirmed"
	CertaintyDifferential Certainty = "differential"
	CertaintyRuledOut     Certainty = "ruled_out"
)

// DiagnosisCode es un código opcional de catálogo.
type DiagnosisCode struct {
	System string `json:"system"`
	Value  string `json:"value"`
}

// Diagnosis es un diagnóstico de la parte A.
type Diagnosis struct {
	Text      string         `json:"text"`
	Code      *DiagnosisCode `json:"code,omitempty"`
	Certainty Certainty      `json:"certainty"`
}

func (d Diagnosis) normalize(ve *kernel.ValidationError, field string) Diagnosis {
	text, err := cleanText(d.Text, MaxDiagnosisTextLen, false)
	if err != nil || text == "" {
		ve.Add(field+".text", "INVALID")
	}
	switch d.Certainty {
	case CertaintyPresumptive, CertaintyConfirmed, CertaintyDifferential, CertaintyRuledOut:
	default:
		ve.Add(field+".certainty", "INVALID")
	}
	out := Diagnosis{Text: text, Certainty: d.Certainty}
	if d.Code != nil {
		sys, err1 := cleanText(d.Code.System, 40, false)
		val, err2 := cleanText(d.Code.Value, 40, false)
		if err1 != nil || err2 != nil || sys == "" || val == "" {
			ve.Add(field+".code", "INVALID")
		}
		out.Code = &DiagnosisCode{System: sys, Value: val}
	}
	return out
}

// DoseUnit es la unidad de dosis.
type DoseUnit string

var doseUnits = map[DoseUnit]bool{
	"mg": true, "ml": true, "ui": true, "tablet": true, "capsule": true, "drop": true,
	"mg_per_kg": true, "ml_per_kg": true,
}

// Route es la vía de administración.
type Route string

var routes = map[Route]bool{
	"oral": true, "sc": true, "im": true, "iv": true, "topical": true, "ophthalmic": true,
	"otic": true, "inhaled": true, "rectal": true,
}

// Frequency es la pauta: "una vez" o cada N horas/días.
type Frequency struct {
	Once  bool   `json:"once,omitempty"`
	Every int    `json:"every,omitempty"`
	Unit  string `json:"unit,omitempty"` // hours | days
}

func (f Frequency) valid() bool {
	if f.Once {
		return f.Every == 0 && f.Unit == ""
	}
	switch f.Unit {
	case "hours":
		return f.Every >= 1 && f.Every <= 72
	case "days":
		return f.Every >= 1 && f.Every <= 90
	}
	return false
}

// MedicationItem es un medicamento estructurado del plan (base de
// Prescription en F4).
type MedicationItem struct {
	MedicationName string    `json:"medication_name"`
	Presentation   string    `json:"presentation,omitempty"`
	Dose           float64   `json:"dose"`
	DoseUnit       DoseUnit  `json:"dose_unit"`
	Route          Route     `json:"route"`
	Frequency      Frequency `json:"frequency"`
	DurationDays   *int      `json:"duration_days,omitempty"`
	Instructions   string    `json:"instructions,omitempty"`
}

func (m MedicationItem) normalize(ve *kernel.ValidationError, field string) MedicationItem {
	name, err := cleanText(m.MedicationName, MaxMedicationNameLen, false)
	if err != nil || name == "" {
		ve.Add(field+".medication_name", "INVALID")
	}
	pres, err := cleanText(m.Presentation, MaxMedicationNameLen, false)
	if err != nil {
		ve.Add(field+".presentation", "INVALID")
	}
	if math.IsNaN(m.Dose) || m.Dose <= 0 || m.Dose > 100000 || !hasMaxDecimals(m.Dose, 3) {
		ve.Add(field+".dose", "OUT_OF_RANGE")
	}
	if !doseUnits[m.DoseUnit] {
		ve.Add(field+".dose_unit", "INVALID")
	}
	if !routes[m.Route] {
		ve.Add(field+".route", "INVALID")
	}
	if !m.Frequency.valid() {
		ve.Add(field+".frequency", "INVALID")
	}
	if !inRangeI(m.DurationDays, 1, 365) {
		ve.Add(field+".duration_days", "OUT_OF_RANGE")
	}
	instr, err := cleanText(m.Instructions, MaxInstructionsLen, true)
	if err != nil {
		ve.Add(field+".instructions", "INVALID")
	}
	m.MedicationName, m.Presentation, m.Instructions = name, pres, instr
	return m
}

// Procedure es un procedimiento realizado.
type Procedure struct {
	Name  string `json:"name"`
	Notes string `json:"notes,omitempty"`
}

// Plan es la parte P del SOAP.
type Plan struct {
	Medications     []MedicationItem `json:"medications"`
	Procedures      []Procedure      `json:"procedures"`
	Recommendations Narrative        `json:"recommendations,omitempty"`
}

func (p Plan) normalize(ve *kernel.ValidationError) Plan {
	if len(p.Medications) > MaxMedications {
		ve.Add("plan.medications", "TOO_MANY")
	}
	if len(p.Procedures) > MaxProcedures {
		ve.Add("plan.procedures", "TOO_MANY")
	}
	out := Plan{Medications: make([]MedicationItem, 0, len(p.Medications)), Procedures: make([]Procedure, 0, len(p.Procedures))}
	for i, m := range p.Medications {
		out.Medications = append(out.Medications, m.normalize(ve, fieldIdx("plan.medications", i)))
	}
	for i, pr := range p.Procedures {
		field := fieldIdx("plan.procedures", i)
		name, err := cleanText(pr.Name, MaxMedicationNameLen, false)
		if err != nil || name == "" {
			ve.Add(field+".name", "INVALID")
		}
		notes, err := cleanText(pr.Notes, MaxProcedureNotesLen, true)
		if err != nil {
			ve.Add(field+".notes", "INVALID")
		}
		out.Procedures = append(out.Procedures, Procedure{Name: name, Notes: notes})
	}
	rec, err := NewNarrative(string(p.Recommendations))
	if err != nil {
		ve.Add("plan.recommendations", "INVALID")
	}
	out.Recommendations = rec
	return out
}

// FollowUpRecommendation es el control recomendado (PRD-007).
type FollowUpRecommendation struct {
	DueDate kernel.CivilDate `json:"due_date"`
	Reason  string           `json:"reason"`
}

// ContentType es el tipo de un adjunto clínico.
type ContentType string

// Tipos admitidos (PRD-004, SEC-003).
const (
	ContentJPEG ContentType = "jpeg"
	ContentPNG  ContentType = "png"
	ContentWebP ContentType = "webp"
	ContentPDF  ContentType = "pdf"
)

// Attachment es un adjunto clínico. ObjectKey lo asigna app con la clave
// clinical/<pet>/<encounter>/<attachment> (ARQ-011).
type Attachment struct {
	ID                uuid.UUID   `json:"id"`
	ObjectKey         string      `json:"object_key"`
	ContentType       ContentType `json:"content_type"`
	SizeBytes         int64       `json:"size_bytes"`
	Description       string      `json:"description,omitempty"`
	VisibleToGuardian bool        `json:"visible_to_guardian"`
}

func (a Attachment) normalize(ve *kernel.ValidationError, field string, pet, enc uuid.UUID) Attachment {
	if a.ID == uuid.Nil {
		ve.Add(field+".id", "INVALID")
	}
	if a.ObjectKey != AttachmentKey(pet, enc, a.ID) {
		ve.Add(field+".object_key", "INVALID")
	}
	switch a.ContentType {
	case ContentJPEG, ContentPNG, ContentWebP, ContentPDF:
	default:
		ve.Add(field+".content_type", "INVALID")
	}
	if a.SizeBytes <= 0 || a.SizeBytes > MaxAttachmentBytes {
		ve.Add(field+".size_bytes", "OUT_OF_RANGE")
	}
	desc, err := cleanText(a.Description, MaxDescriptionLen, false)
	if err != nil {
		ve.Add(field+".description", "INVALID")
	}
	a.Description = desc
	return a
}

// AttachmentKey es la clave de objeto de un adjunto (ARQ-011).
func AttachmentKey(pet, enc, att uuid.UUID) string {
	return "clinical/" + pet.String() + "/" + enc.String() + "/" + att.String()
}

// Signature es la firma del encuentro. ContentHash en hex (SHA-256).
type Signature struct {
	SignedBy      uuid.UUID `json:"signed_by"`
	SignedAt      time.Time `json:"signed_at"`
	StreamVersion int64     `json:"stream_version"`
	ContentHash   string    `json:"content_hash"`
}

// AddendumReason es el motivo de una adenda (PRD-006).
type AddendumReason string

// Motivos admitidos.
const (
	AddendumCorrection         AddendumReason = "correction"
	AddendumAdditionalFindings AddendumReason = "additional_findings"
	AddendumLabResults         AddendumReason = "lab_results"
	AddendumOther              AddendumReason = "other"
)

// Addendum es una adenda firmada.
type Addendum struct {
	Seq         int            `json:"seq"`
	Text        string         `json:"text"`
	Reason      AddendumReason `json:"reason"`
	SignedBy    uuid.UUID      `json:"signed_by"`
	SignedAt    time.Time      `json:"signed_at"`
	Attachments []Attachment   `json:"attachments"`
}

func fieldIdx(prefix string, i int) string {
	return prefix + "[" + strconv.Itoa(i) + "]"
}
