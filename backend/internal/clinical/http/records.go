package http

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// recordAccess es cómo ve el actor la ficha de una mascota (API-003).
type recordAccess struct {
	level      gen.PetClinicalSummaryAccess
	guardianID uuid.UUID
	org        *uuid.UUID
	// orgIDs restringe a lo propio en custodia (nil = sin restricción).
	orgIDs []uuid.UUID
}

// audience del timeline para el nivel de acceso.
func (r recordAccess) audience() string {
	switch r.level {
	case gen.PetClinicalSummaryAccessGuardian:
		return "guardian"
	case gen.PetClinicalSummaryAccessAdmin:
		return ""
	}
	return "org"
}

// record autoriza record:read sobre la mascota y resuelve el nivel de acceso:
// tutor; miembro de org vinculada (todo lo firmado); miembro de org con
// custodia (solo lo propio); super_admin (auditado). No visible → 404.
func (h *ClinicalHandlers) record(ctx context.Context, p auth.Principal, petID uuid.UUID) (recordAccess, error) {
	guardianID, err := h.Pets.GuardianOf(ctx, petID)
	if err != nil {
		return recordAccess{}, err
	}
	linked, err := h.Pets.LinkedOrgIDs(ctx, petID)
	if err != nil {
		return recordAccess{}, err
	}
	custody, err := h.Pets.CustodyOrgIDs(ctx, petID)
	if err != nil {
		return recordAccess{}, err
	}
	res := authz.PetRecordResource{PetID: petID, GuardianID: guardianID, LinkedOrgIDs: linked, CustodyOrgIDs: custody}
	if d := authz.Can(p, authz.RecordRead, res); !d.Allow {
		return recordAccess{}, &kernel.NotFoundError{Resource: "pet"}
	}
	out := recordAccess{guardianID: guardianID}
	switch {
	case p.UserID == guardianID:
		out.level = gen.PetClinicalSummaryAccessGuardian
	case actingOrg(p, linked...) != nil:
		out.level, out.org = gen.PetClinicalSummaryAccessLinked, actingOrg(p, linked...)
	case actingOrg(p, custody...) != nil:
		out.level, out.org = gen.PetClinicalSummaryAccessCustody, actingOrg(p, custody...)
		for _, id := range custody {
			if actingOrg(p, id) != nil {
				out.orgIDs = append(out.orgIDs, id)
			}
		}
	default:
		out.level = gen.PetClinicalSummaryAccessAdmin
	}
	return out, nil
}

// readRecord = record + auditoría de la lectura (SEC-005).
func (h *ClinicalHandlers) readRecord(ctx context.Context, petID uuid.UUID) (recordAccess, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return recordAccess{}, err
	}
	acc, err := h.record(ctx, p, petID)
	if err != nil {
		return recordAccess{}, err
	}
	if err := h.audit(ctx, p, acc.guardianID, acc.org, petID, "record", petID); err != nil {
		return recordAccess{}, err
	}
	return acc, nil
}

func (h *ClinicalHandlers) lag(ctx context.Context, projection string) float64 {
	if h.Freshness == nil {
		return 0
	}
	lag, err := h.Freshness.LagSeconds(ctx, projection)
	if err != nil {
		h.Logger.WarnContext(ctx, "clinical: no se pudo calcular la frescura", "projection", projection, "error", err)
		return 0
	}
	return lag
}

// GetPetTimeline devuelve el historial cronológico (vista derivada).
func (h *ClinicalHandlers) GetPetTimeline(ctx context.Context, req gen.GetPetTimelineRequestObject) (gen.GetPetTimelineResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	acc, err := h.readRecord(ctx, petID)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	f := app.TimelineFilter{PetID: petID, Cursor: cursor, Limit: limitOf(req.Params.Limit),
		From: req.Params.From, To: req.Params.To, Audience: acc.audience(), OrgIDs: acc.orgIDs}
	if req.Params.Kind != nil {
		f.Kind = string(*req.Params.Kind)
	}
	if req.Params.OrganizationId != nil {
		f.OrganizationID = ptr(uuid.UUID(*req.Params.OrganizationId))
	}
	items, next, err := h.Queries.Timeline(ctx, f)
	if err != nil {
		return nil, err
	}
	lag := h.lag(ctx, app.ProjectionTimeline)
	page := gen.TimelinePage{Data: make([]gen.TimelineItem, 0, len(items)), Freshness: gen.Freshness{LagSeconds: float32(lag)}}
	for _, it := range items {
		dto := gen.TimelineItem{
			OccurredAt: it.OccurredAt, Kind: gen.TimelineItemKind(it.Kind), RefId: openapi_types.UUID(it.RefID),
			Title: it.Title, Summary: optStr(it.Summary), Visibility: gen.TimelineItemVisibility(it.Visibility),
		}
		if it.OrganizationID != nil {
			dto.OrganizationId = ptr(openapi_types.UUID(*it.OrganizationID))
		}
		page.Data = append(page.Data, dto)
	}
	if next != nil {
		page.NextCursor = ptr(encodeCursor(*next))
	}
	return gen.GetPetTimeline200JSONResponse{Body: page, Headers: gen.GetPetTimeline200ResponseHeaders{XPPFreshness: freshnessHeader(lag)}}, nil
}

// ListPetEncounters lista las consultas firmadas visibles (custodia incluida).
func (h *ClinicalHandlers) ListPetEncounters(ctx context.Context, req gen.ListPetEncountersRequestObject) (gen.ListPetEncountersResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	acc, err := h.readRecord(ctx, petID)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	items, next, err := h.Queries.ListByPet(ctx, petID, acc.orgIDs, true, cursor, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	page := gen.EncounterPage{Data: make([]gen.EncounterSummary, 0, len(items))}
	for _, it := range items {
		page.Data = append(page.Data, toSummary(it))
	}
	if next != nil {
		page.NextCursor = ptr(encodeCursor(*next))
	}
	return gen.ListPetEncounters200JSONResponse(page), nil
}

// GetPetWeightHistory devuelve las mediciones y la tendencia (PRD-025).
func (h *ClinicalHandlers) GetPetWeightHistory(ctx context.Context, req gen.GetPetWeightHistoryRequestObject) (gen.GetPetWeightHistoryResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	acc, err := h.readRecord(ctx, petID)
	if err != nil {
		return nil, err
	}
	points, trend, err := h.Queries.WeightHistory(ctx, petID, acc.orgIDs)
	if err != nil {
		return nil, err
	}
	lag := h.lag(ctx, app.ProjectionWeight)
	body := gen.WeightHistory{Data: make([]gen.WeightPoint, 0, len(points)), Freshness: gen.Freshness{LagSeconds: float32(lag)}}
	for _, pt := range points {
		body.Data = append(body.Data, toWeightPoint(pt))
	}
	body.Trend.WindowDays = 90
	if trend != nil {
		body.Trend.WindowDays = trend.WindowDays
		body.Trend.Alert = trend.Alert
		if trend.DeltaKg != nil {
			body.Trend.DeltaKg = ptr(float32(*trend.DeltaKg))
		}
		if trend.DeltaPct != nil {
			body.Trend.DeltaPct = ptr(float32(*trend.DeltaPct))
		}
	}
	return gen.GetPetWeightHistory200JSONResponse{Body: body, Headers: gen.GetPetWeightHistory200ResponseHeaders{XPPFreshness: freshnessHeader(lag)}}, nil
}

// GetPetSummary compone la ficha de paciente (FE-005).
func (h *ClinicalHandlers) GetPetSummary(ctx context.Context, req gen.GetPetSummaryRequestObject) (gen.GetPetSummaryResponseObject, error) {
	petID := uuid.UUID(req.PetId)
	acc, err := h.readRecord(ctx, petID)
	if err != nil {
		return nil, err
	}
	s, err := h.Queries.Summary(ctx, petID, acc.orgIDs)
	if err != nil {
		return nil, err
	}
	alerts, err := h.Pets.Alerts(ctx, petID)
	if err != nil {
		return nil, err
	}
	lag := max(h.lag(ctx, app.ProjectionWeight), h.lag(ctx, app.ProjectionTimeline))
	body := gen.PetClinicalSummary{
		Access:        ptr(acc.level),
		Alerts:        slices.Clip(append([]string{}, alerts...)),
		Freshness:     gen.Freshness{LagSeconds: float32(lag)},
		OpenFollowUps: toFollowUps(s.OpenFollowUps),
		NextDue:       []map[string]interface{}{},
	}
	for _, f := range toFollowUps(s.NextDue) {
		body.NextDue = append(body.NextDue, map[string]interface{}{
			"kind": "follow_up", "due_date": f.DueDate.Format("2006-01-02"), "reason": f.Reason,
			"encounter_id": f.EncounterId.String(),
		})
	}
	if s.LastEncounter != nil {
		body.LastEncounter = ptr(toSummary(*s.LastEncounter))
	}
	if s.CurrentWeight != nil {
		body.CurrentWeight = ptr(toWeightPoint(*s.CurrentWeight))
	}
	return gen.GetPetSummary200JSONResponse{Body: body, Headers: gen.GetPetSummary200ResponseHeaders{XPPFreshness: freshnessHeader(lag)}}, nil
}

// --------------------------------------------------------- documentos

// GetEncounterPdf redirige al PDF firmado o informa que está en generación
// (202 DocumentJob, DOCUMENT_PENDING).
func (h *ClinicalHandlers) GetEncounterPdf(ctx context.Context, req gen.GetEncounterPdfRequestObject) (gen.GetEncounterPdfResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	acc, res, err := h.load(ctx, p, uuid.UUID(req.EncounterId), authz.EncounterRead)
	if err != nil {
		return nil, err
	}
	if h.Documents == nil {
		return nil, storageUnavailable()
	}
	job, err := h.Documents.LatestForRef(ctx, app.DocumentJobKindEncounterPDF, acc.ID)
	if err != nil {
		return nil, err // sin trabajo = no firmada → 404
	}
	org := actingOrg(p, append([]uuid.UUID{acc.OrganizationID}, res.LinkedOrgIDs...)...)
	if err := h.audit(ctx, p, acc.GuardianID, org, acc.PetID, "pdf", acc.ID); err != nil {
		return nil, err
	}
	if job.Status == "ready" && job.ObjectKey != "" && h.Objects != nil {
		url, err := h.presignPDF(ctx, job.ObjectKey, acc.ID)
		if err != nil {
			return nil, err
		}
		if wantsJSON(req.Params.Accept) {
			return gen.GetEncounterPdf200JSONResponse{Url: url, ExpiresAt: h.now().Add(presignTTL)}, nil
		}
		return gen.GetEncounterPdf302Response{Headers: gen.GetEncounterPdf302ResponseHeaders{Location: &url}}, nil
	}
	return gen.GetEncounterPdf202JSONResponse(toDocumentJob(job, "", nil)), nil
}

// GetDocumentJob consulta el estado de un documento en generación (API-013).
func (h *ClinicalHandlers) GetDocumentJob(ctx context.Context, req gen.GetDocumentJobRequestObject) (gen.GetDocumentJobResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if h.Documents == nil {
		return nil, storageUnavailable()
	}
	job, err := h.Documents.Job(ctx, uuid.UUID(req.JobId))
	if err != nil {
		return nil, err
	}
	var access error
	switch job.Kind {
	case app.DocumentJobKindEncounterPDF:
		_, _, access = h.load(ctx, p, job.RefID, authz.EncounterRead)
	case app.DocumentJobKindCardPDF:
		_, access = h.record(ctx, p, job.RefID)
	default:
		return nil, &kernel.NotFoundError{Resource: "document"}
	}
	if err := access; err != nil {
		var nf *kernel.NotFoundError
		var fb *kernel.ForbiddenError
		if errors.As(err, &nf) || errors.As(err, &fb) {
			return nil, &kernel.NotFoundError{Resource: "document"}
		}
		return nil, err
	}
	url := ""
	exp := h.now().Add(presignTTL)
	if job.Status == "ready" && job.ObjectKey != "" && h.Objects != nil {
		if url, err = h.presignPDF(ctx, job.ObjectKey, job.RefID); err != nil {
			return nil, err
		}
	}
	return gen.GetDocumentJob200JSONResponse(toDocumentJob(job, url, &exp)), nil
}
