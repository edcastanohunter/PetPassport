package http

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func ptr[T any](v T) *T { return &v }

func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	return ptr(float32(*v))
}

func f64(v *float32) *float64 {
	if v == nil {
		return nil
	}
	return ptr(float64(*v))
}

func strOr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// viewOpts controla qué ve el actor del DTO (API-001: tutor solo adjuntos
// visibles y sin avisos clínicos).
type viewOpts struct {
	guardian bool
	pending  []app.StagedUpload
	pdfURL   string
	pdfExp   *time.Time
}

// toEncounter mapea la vista al DTO del contrato.
func toEncounter(v app.EncounterView, o viewOpts) gen.Encounter {
	out := gen.Encounter{
		Id:             openapi_types.UUID(v.ID),
		Version:        v.Version,
		Pet:            gen.PetRef{Id: openapi_types.UUID(v.PetID), Name: v.PetName, Species: v.PetSpecies},
		OrganizationId: openapi_types.UUID(v.OrganizationID),
		Type:           gen.EncounterType(v.Type),
		Status:         gen.EncounterStatus(v.Status),
		StartedAt:      v.StartedAt,
		Reason:         v.Reason,
		Subjective:     v.Subjective,
		Diagnoses:      toDiagnoses(v.Diagnoses),
		Plan:           toPlan(v.Plan),
		Attachments:    []gen.Attachment{},
		Addenda:        []gen.Addendum{},
		CreatedAt:      v.CreatedAt,
		UpdatedAt:      v.UpdatedAt,
	}
	if v.AppointmentID != nil {
		out.AppointmentId = ptr(openapi_types.UUID(*v.AppointmentID))
	}
	if v.AttendingVetID != nil {
		out.AttendingVet = &gen.UserRef{Id: openapi_types.UUID(*v.AttendingVetID), DisplayName: v.AttendingVetName}
	}
	if v.Vitals != nil {
		out.Vitals = toVitals(*v.Vitals)
		if !o.guardian {
			ws := domain.VitalsWarnings(v.PetSpecies, *v.Vitals)
			if len(ws) > 0 {
				list := make([]gen.VitalWarning, 0, len(ws))
				for _, w := range ws {
					list = append(list, gen.VitalWarning{Field: w.Field, Code: gen.VitalWarningCode(w.Code), Message: w.Message})
				}
				out.Warnings = &list
			}
		}
	}
	if v.Objective != nil {
		out.Objective = toObjective(*v.Objective)
	}
	if v.FollowUp != nil {
		if d, err := time.Parse("2006-01-02", string(v.FollowUp.DueDate)); err == nil {
			out.FollowUp = &gen.FollowUp{DueDate: openapi_types.Date{Time: d}, Reason: v.FollowUp.Reason}
		}
	}
	if v.Status == string(domain.StatusDraft) {
		out.Abandoned = ptr(v.Abandoned)
	}
	byAddendum := map[int][]gen.Attachment{}
	for _, a := range v.Attachments {
		if o.guardian && !a.VisibleToGuardian {
			continue
		}
		dto := toAttachment(a)
		if a.AddendumSeq != nil {
			byAddendum[*a.AddendumSeq] = append(byAddendum[*a.AddendumSeq], dto)
			continue
		}
		out.Attachments = append(out.Attachments, dto)
	}
	if !o.guardian {
		for _, s := range o.pending {
			dto := toAttachment(stagedView(s))
			dto.PendingAddendum = ptr(true)
			out.Attachments = append(out.Attachments, dto)
		}
	}
	if v.Signature != nil {
		out.Signature = &gen.Signature{
			SignedBy:      userRef(v, v.Signature.SignedBy),
			SignedAt:      v.Signature.SignedAt,
			StreamVersion: v.Signature.StreamVersion,
			ContentHash:   v.Signature.ContentHash,
		}
	}
	for _, ad := range v.Addenda {
		atts := byAddendum[ad.Seq]
		if atts == nil {
			atts = []gen.Attachment{}
		}
		out.Addenda = append(out.Addenda, gen.Addendum{
			Seq: ad.Seq, Text: ad.Text, Reason: gen.AddendumReason(ad.Reason),
			SignedBy: userRef(v, ad.SignedBy), SignedAt: ad.SignedAt, Attachments: atts,
		})
	}
	if v.PDF != nil {
		pdf := &gen.EncounterPdf{Status: gen.EncounterPdfStatus(v.PDF.Status)}
		if v.PDF.Version > 0 {
			pdf.Version = ptr(v.PDF.Version)
		}
		if o.pdfURL != "" {
			pdf.Url = &o.pdfURL
			pdf.ExpiresAt = o.pdfExp
		}
		out.Pdf = pdf
	}
	return out
}

// userRef resuelve el nombre del firmante. El read model solo desnormaliza
// el nombre del responsable (firma y adendas son de veterinarios de la org;
// si no coincide se devuelve sin nombre).
func userRef(v app.EncounterView, id uuid.UUID) gen.UserRef {
	ref := gen.UserRef{Id: openapi_types.UUID(id)}
	if v.AttendingVetID != nil && *v.AttendingVetID == id {
		ref.DisplayName = v.AttendingVetName
	}
	return ref
}

func stagedView(s app.StagedUpload) app.AttachmentView {
	return app.AttachmentView{
		ID: s.Attachment.ID, ObjectKey: s.Attachment.ObjectKey, ContentType: string(s.Attachment.ContentType),
		SizeBytes: s.Attachment.SizeBytes, Description: s.Attachment.Description,
		VisibleToGuardian: s.Attachment.VisibleToGuardian, CreatedAt: s.CreatedAt,
	}
}

func toAttachment(a app.AttachmentView) gen.Attachment {
	return gen.Attachment{
		Id:                openapi_types.UUID(a.ID),
		ContentType:       gen.AttachmentContentType(a.ContentType),
		Size:              int(a.SizeBytes),
		Description:       optStr(a.Description),
		VisibleToGuardian: a.VisibleToGuardian,
		AddendumSeq:       a.AddendumSeq,
		CreatedAt:         a.CreatedAt,
	}
}

func toVitals(v domain.Vitals) *gen.Vitals {
	out := &gen.Vitals{
		WeightKg: f32(v.WeightKg), TempC: f32(v.TempC), HeartRateBpm: v.HeartRateBpm,
		RespRateRpm: v.RespRateRpm, BodyCondition: v.BodyCondition,
		CrtSeconds: f32(v.CRTSeconds), HydrationPct: f32(v.HydrationPct),
	}
	if v.Mucosa != nil {
		out.Mucosa = ptr(gen.VitalsMucosa(*v.Mucosa))
	}
	return out
}

func fromVitals(v gen.Vitals) domain.Vitals {
	out := domain.Vitals{
		WeightKg: f64(v.WeightKg), TempC: f64(v.TempC), HeartRateBpm: v.HeartRateBpm,
		RespRateRpm: v.RespRateRpm, BodyCondition: v.BodyCondition,
		CRTSeconds: f64(v.CrtSeconds), HydrationPct: f64(v.HydrationPct),
	}
	if v.Mucosa != nil {
		out.Mucosa = ptr(domain.Mucosa(*v.Mucosa))
	}
	return out
}

func toObjective(o domain.Objective) *gen.Objective {
	out := &gen.Objective{Narrative: optStr(string(o.Narrative))}
	if len(o.Systems) > 0 {
		m := make(map[string]gen.Finding, len(o.Systems))
		for k, f := range o.Systems {
			m[string(k)] = gen.Finding{Status: gen.FindingStatus(f.Status), Note: optStr(f.Note)}
		}
		out.Systems = &m
	}
	return out
}

func fromObjective(o gen.Objective) domain.Objective {
	out := domain.Objective{Narrative: domain.Narrative(strOr(o.Narrative))}
	if o.Systems != nil {
		out.Systems = make(map[domain.BodySystem]domain.Finding, len(*o.Systems))
		for k, f := range *o.Systems {
			out.Systems[domain.BodySystem(k)] = domain.Finding{Status: domain.FindingStatus(f.Status), Note: strOr(f.Note)}
		}
	}
	return out
}

func toDiagnoses(ds []domain.Diagnosis) []gen.Diagnosis {
	out := make([]gen.Diagnosis, 0, len(ds))
	for _, d := range ds {
		g := gen.Diagnosis{Text: d.Text, Certainty: gen.DiagnosisCertainty(d.Certainty)}
		if d.Code != nil {
			g.Code = &struct {
				System string `json:"system"`
				Value  string `json:"value"`
			}{System: d.Code.System, Value: d.Code.Value}
		}
		out = append(out, g)
	}
	return out
}

func fromDiagnoses(ds []gen.Diagnosis) []domain.Diagnosis {
	out := make([]domain.Diagnosis, 0, len(ds))
	for _, d := range ds {
		x := domain.Diagnosis{Text: d.Text, Certainty: domain.Certainty(d.Certainty)}
		if d.Code != nil {
			x.Code = &domain.DiagnosisCode{System: d.Code.System, Value: d.Code.Value}
		}
		out = append(out, x)
	}
	return out
}

func toPlan(p domain.Plan) gen.Plan {
	meds := make([]gen.MedicationItem, 0, len(p.Medications))
	for _, m := range p.Medications {
		f := gen.Frequency{}
		if m.Frequency.Once {
			f.Once = ptr(true)
		} else {
			f.Every = ptr(m.Frequency.Every)
			f.Unit = ptr(gen.FrequencyUnit(m.Frequency.Unit))
		}
		meds = append(meds, gen.MedicationItem{
			MedicationName: m.MedicationName, Presentation: optStr(m.Presentation),
			Dose: float32(m.Dose), DoseUnit: gen.MedicationItemDoseUnit(m.DoseUnit),
			Route: gen.MedicationItemRoute(m.Route), Frequency: f,
			DurationDays: m.DurationDays, Instructions: optStr(m.Instructions),
		})
	}
	procs := make([]gen.Procedure, 0, len(p.Procedures))
	for _, pr := range p.Procedures {
		procs = append(procs, gen.Procedure{Name: pr.Name, Notes: optStr(pr.Notes)})
	}
	return gen.Plan{Medications: &meds, Procedures: &procs, Recommendations: optStr(string(p.Recommendations))}
}

func fromPlan(p gen.Plan) domain.Plan {
	out := domain.Plan{Medications: []domain.MedicationItem{}, Procedures: []domain.Procedure{},
		Recommendations: domain.Narrative(strOr(p.Recommendations))}
	if p.Medications != nil {
		for _, m := range *p.Medications {
			f := domain.Frequency{}
			if m.Frequency.Once != nil {
				f.Once = *m.Frequency.Once
			}
			if m.Frequency.Every != nil {
				f.Every = *m.Frequency.Every
			}
			if m.Frequency.Unit != nil {
				f.Unit = string(*m.Frequency.Unit)
			}
			out.Medications = append(out.Medications, domain.MedicationItem{
				MedicationName: m.MedicationName, Presentation: strOr(m.Presentation),
				Dose: float64(m.Dose), DoseUnit: domain.DoseUnit(m.DoseUnit), Route: domain.Route(m.Route),
				Frequency: f, DurationDays: m.DurationDays, Instructions: strOr(m.Instructions),
			})
		}
	}
	if p.Procedures != nil {
		for _, pr := range *p.Procedures {
			out.Procedures = append(out.Procedures, domain.Procedure{Name: pr.Name, Notes: strOr(pr.Notes)})
		}
	}
	return out
}

func toSummary(it app.EncounterListItem) gen.EncounterSummary {
	s := gen.EncounterSummary{
		Id:             openapi_types.UUID(it.ID),
		Version:        it.Version,
		Pet:            gen.PetRef{Id: openapi_types.UUID(it.PetID), Name: it.PetName, Species: it.PetSpecies},
		OrganizationId: openapi_types.UUID(it.OrganizationID),
		Type:           gen.EncounterType(it.Type),
		Status:         gen.EncounterStatus(it.Status),
		StartedAt:      it.StartedAt,
		Reason:         it.Reason,
		SignedAt:       it.SignedAt,
		LastActivityAt: it.LastActivityAt,
		Abandoned:      it.Status == string(domain.StatusDraft) && time.Since(it.LastActivityAt) > domain.AbandonAfter,
	}
	if it.AttendingVetID != nil {
		s.AttendingVetId = ptr(openapi_types.UUID(*it.AttendingVetID))
	}
	return s
}

func toWeightPoint(p app.WeightPoint) gen.WeightPoint {
	w := gen.WeightPoint{MeasuredAt: p.MeasuredAt, WeightKg: float32(p.WeightKg), Source: gen.WeightPointSource(p.Source)}
	if p.OrganizationID != nil {
		w.OrganizationId = ptr(openapi_types.UUID(*p.OrganizationID))
	}
	return w
}

func toFollowUps(items []app.FollowUpItem) []gen.FollowUpItem {
	out := make([]gen.FollowUpItem, 0, len(items))
	for _, f := range items {
		d, err := time.Parse("2006-01-02", f.DueDate)
		if err != nil {
			continue
		}
		out = append(out, gen.FollowUpItem{
			EncounterId: openapi_types.UUID(f.EncounterID), OrganizationId: openapi_types.UUID(f.OrganizationID),
			DueDate: openapi_types.Date{Time: d}, Reason: f.Reason,
		})
	}
	return out
}

func toDocumentJob(j app.DocumentJobView, url string, exp *time.Time) gen.DocumentJob {
	d := gen.DocumentJob{Id: openapi_types.UUID(j.ID), Status: gen.DocumentJobStatus(j.Status)}
	if url != "" {
		d.Url = &url
		d.ExpiresAt = exp
	}
	return d
}

// ------------------------------------------------------------- cursores

func encodeCursor(c app.Cursor) string {
	raw, _ := json.Marshal(map[string]any{"t": c.StartedAt.UTC().Format(time.RFC3339Nano), "id": c.ID.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s *string) (app.Cursor, error) {
	if s == nil || *s == "" {
		return app.Cursor{}, nil
	}
	invalid := &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "cursor", Code: "INVALID"}}}
	raw, err := base64.RawURLEncoding.DecodeString(*s)
	if err != nil {
		return app.Cursor{}, invalid
	}
	var m struct {
		T  string `json:"t"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return app.Cursor{}, invalid
	}
	t, err := time.Parse(time.RFC3339Nano, m.T)
	if err != nil {
		return app.Cursor{}, invalid
	}
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return app.Cursor{}, invalid
	}
	return app.Cursor{StartedAt: t, ID: id}, nil
}

func limitOf(l *int) int {
	if l == nil {
		return 20
	}
	return *l
}

// freshnessHeader formatea X-PP-Freshness (API-013).
func freshnessHeader(lag float64) *string {
	return ptr("lag=" + formatLag(lag))
}

func formatLag(lag float64) string {
	b, _ := json.Marshal(float64(int64(lag*10)) / 10)
	return string(b)
}
