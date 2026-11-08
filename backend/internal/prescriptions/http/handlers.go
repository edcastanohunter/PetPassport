package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/queue"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// EncounterAccessReader permite comprobar la autorización de un encuentro clínico.
type EncounterAccessReader interface {
	GetEncounterAccess(ctx context.Context, id uuid.UUID) (*authz.EncounterResource, error)
}

// PetAccessReader permite consultar información de la mascota para authz (PP-F4-API-003).
type PetAccessReader interface {
	GetPetInfo(ctx context.Context, petID uuid.UUID) (name string, species string, guardianID uuid.UUID, err error)
}

// OCREnqueuer encola trabajos en la cola asynq de OCR (PP-F4-ARQ-012).
type OCREnqueuer interface {
	EnqueueOCRPipeline(ctx context.Context, p queue.OCRPipelinePayload) (*asynq.TaskInfo, error)
}

// PrescriptionHandlers implementa los endpoints de recetas emitidas y verificación pública (PP-F4-API-002, API-003, API-004).
type PrescriptionHandlers struct {
	svc             *app.Service
	storage         storage.ObjectStorage
	encounterReader EncounterAccessReader
	petReader       PetAccessReader
	enqueuer        OCREnqueuer
}

// NewPrescriptionHandlers construye los handlers de prescripciones.
func NewPrescriptionHandlers(svc *app.Service, storage storage.ObjectStorage) *PrescriptionHandlers {
	return &PrescriptionHandlers{
		svc:     svc,
		storage: storage,
	}
}

// WithEncounterReader asocia el lector de acceso a encuentros para verificación de permisos.
func (h *PrescriptionHandlers) WithEncounterReader(r EncounterAccessReader) *PrescriptionHandlers {
	h.encounterReader = r
	return h
}

// WithPetReader asocia el lector de información de mascotas.
func (h *PrescriptionHandlers) WithPetReader(r PetAccessReader) *PrescriptionHandlers {
	h.petReader = r
	return h
}

// WithEnqueuer asocia el cliente encolador asynq.
func (h *PrescriptionHandlers) WithEnqueuer(e OCREnqueuer) *PrescriptionHandlers {
	h.enqueuer = e
	return h
}

func (h *PrescriptionHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "unauthenticated"}
	}
	return p, nil
}

// DraftPrescription inicia un borrador de receta (PP-F4-API-002).
func (h *PrescriptionHandlers) DraftPrescription(ctx context.Context, req gen.DraftPrescriptionRequestObject) (gen.DraftPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PrescriptionIssue, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	body := req.Body
	if body == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	var items []domain.PrescriptionItem
	if body.Items != nil {
		items = make([]domain.PrescriptionItem, 0, len(*body.Items))
		for _, it := range *body.Items {
			items = append(items, toDomainItem(it))
		}
	}

	var validUntil *kernel.CivilDate
	refillsAllowed := 0
	if body.Validity != nil {
		if body.Validity.ValidUntil != nil {
			vu, err := kernel.NewCivilDate(body.Validity.ValidUntil.String())
			if err != nil {
				ve := &kernel.ValidationError{}
				ve.Add("validity.valid_until", "INVALID_DATE")
				return nil, ve
			}
			validUntil = &vu
		}
		if body.Validity.RefillsAllowed != nil {
			refillsAllowed = *body.Validity.RefillsAllowed
		}
	}

	cmd := app.DraftIssuedPrescriptionCmd{
		PetID:          body.PetId,
		IssuerOrgID:    req.OrgId,
		Country:        strings.ToUpper(strings.TrimSpace(body.Country)),
		EncounterID:    body.EncounterId,
		Items:          items,
		RefillsAllowed: refillsAllowed,
		ValidUntil:     validUntil,
		Notes:          body.Notes,
	}

	rxID, err := h.svc.DraftIssuedPrescription(ctx, cmd, p.UserID)
	if err != nil {
		return nil, err
	}

	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}

	return gen.DraftPrescription201JSONResponse(toGenPrescriptionDetail(detail)), nil
}

// ListOrgPrescriptions lista las recetas emitidas por la clínica (PP-F4-API-002).
func (h *PrescriptionHandlers) ListOrgPrescriptions(ctx context.Context, req gen.ListOrgPrescriptionsRequestObject) (gen.ListOrgPrescriptionsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PrescriptionList, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = *req.Params.Status
	}
	limit := limitOf(req.Params.Limit)

	summaries, err := h.svc.ListByOrg(ctx, req.OrgId, status, req.Params.PetId, req.Params.IssuerId, limit)
	if err != nil {
		return nil, err
	}

	data := make([]gen.PrescriptionSummary, 0, len(summaries))
	for _, s := range summaries {
		data = append(data, toGenSummary(s))
	}

	return gen.ListOrgPrescriptions200JSONResponse{Data: data}, nil
}

// GetPrescription devuelve el detalle de una receta (PP-F4-API-002).
func (h *PrescriptionHandlers) GetPrescription(ctx context.Context, req gen.GetPrescriptionRequestObject) (gen.GetPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detail, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionRead, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	ver := detail.Version
	return gen.GetPrescription200JSONResponse{
		Body: toGenPrescriptionDetail(detail),
		Headers: gen.GetPrescription200ResponseHeaders{
			ETag: &ver,
		},
	}, nil
}

// UpdatePrescriptionDraft modifica un borrador de receta (PP-F4-API-002).
func (h *PrescriptionHandlers) UpdatePrescriptionDraft(ctx context.Context, req gen.UpdatePrescriptionDraftRequestObject) (gen.UpdatePrescriptionDraftResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detailBefore, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	res := authz.PrescriptionResource{
		ID:          detailBefore.ID,
		PetID:       detailBefore.PetID,
		GuardianID:  detailBefore.GuardianID,
		IssuerOrgID: detailBefore.IssuerOrgID,
		IssuerID:    detailBefore.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionIssue, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Params.IfMatch != 0 && int64(req.Params.IfMatch) != detailBefore.Version {
		return nil, &kernel.ConcurrencyError{Expected: int64(req.Params.IfMatch), Actual: detailBefore.Version}
	}

	body := req.Body
	if body == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	var items []domain.PrescriptionItem
	if body.Items != nil {
		items = make([]domain.PrescriptionItem, 0, len(*body.Items))
		for _, it := range *body.Items {
			items = append(items, toDomainItem(it))
		}
	}

	var validUntil *kernel.CivilDate
	refillsAllowed := 0
	if body.Validity != nil {
		if body.Validity.ValidUntil != nil {
			vu, err := kernel.NewCivilDate(body.Validity.ValidUntil.String())
			if err != nil {
				ve := &kernel.ValidationError{}
				ve.Add("validity.valid_until", "INVALID_DATE")
				return nil, ve
			}
			validUntil = &vu
		}
		if body.Validity.RefillsAllowed != nil {
			refillsAllowed = *body.Validity.RefillsAllowed
		}
	}

	cmd := app.UpdateDraftCmd{
		PrescriptionID: req.PrescriptionId,
		Items:          items,
		RefillsAllowed: refillsAllowed,
		ValidUntil:     validUntil,
		Notes:          body.Notes,
	}

	if err := h.svc.UpdateDraft(ctx, cmd, p.UserID); err != nil {
		return nil, err
	}

	detailAfter, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	verAfter := detailAfter.Version
	return gen.UpdatePrescriptionDraft200JSONResponse{
		Body: toGenPrescriptionDetail(detailAfter),
		Headers: gen.UpdatePrescriptionDraft200ResponseHeaders{
			ETag: &verAfter,
		},
	}, nil
}

// IssuePrescription firma y formaliza la emisión de la receta (PP-F4-API-002, DOM-005).
func (h *PrescriptionHandlers) IssuePrescription(ctx context.Context, req gen.IssuePrescriptionRequestObject) (gen.IssuePrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detailBefore, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	res := authz.PrescriptionResource{
		ID:          detailBefore.ID,
		PetID:       detailBefore.PetID,
		GuardianID:  detailBefore.GuardianID,
		IssuerOrgID: detailBefore.IssuerOrgID,
		IssuerID:    detailBefore.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionIssue, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Params.IfMatch != 0 && int64(req.Params.IfMatch) != detailBefore.Version {
		return nil, &kernel.ConcurrencyError{Expected: int64(req.Params.IfMatch), Actual: detailBefore.Version}
	}

	signerName := ""
	if req.Body != nil && req.Body.SignerName != nil {
		signerName = *req.Body.SignerName
	}

	rxID, err := h.svc.IssuePrescription(ctx, app.IssuePrescriptionCmd{
		PrescriptionID: req.PrescriptionId,
		SignerName:     signerName,
	}, p.UserID)
	if err != nil {
		return nil, err
	}

	detailAfter, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}

	verAfter := detailAfter.Version
	return gen.IssuePrescription200JSONResponse{
		Body: toGenPrescriptionDetail(detailAfter),
		Headers: gen.IssuePrescription200ResponseHeaders{
			ETag: &verAfter,
		},
	}, nil
}

// RevokePrescription revoca una receta médica emitida (PP-F4-API-002, DOM-004).
func (h *PrescriptionHandlers) RevokePrescription(ctx context.Context, req gen.RevokePrescriptionRequestObject) (gen.RevokePrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detailBefore, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	res := authz.PrescriptionResource{
		ID:          detailBefore.ID,
		PetID:       detailBefore.PetID,
		GuardianID:  detailBefore.GuardianID,
		IssuerOrgID: detailBefore.IssuerOrgID,
		IssuerID:    detailBefore.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionRevoke, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || strings.TrimSpace(req.Body.Reason) == "" {
		ve := &kernel.ValidationError{}
		ve.Add("reason", "REQUIRED")
		return nil, ve
	}

	if err := h.svc.RevokePrescription(ctx, app.RevokePrescriptionCmd{
		PrescriptionID: req.PrescriptionId,
		Reason:         req.Body.Reason,
	}, p.UserID); err != nil {
		return nil, err
	}

	detailAfter, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	return gen.RevokePrescription200JSONResponse(toGenPrescriptionDetail(detailAfter)), nil
}

// GetPrescriptionPDF obtiene la URL prefirmada del PDF de la receta (PP-F4-API-002, ARQ-011).
func (h *PrescriptionHandlers) GetPrescriptionPDF(ctx context.Context, req gen.GetPrescriptionPDFRequestObject) (gen.GetPrescriptionPDFResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detail, err := h.svc.GetByID(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}

	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionRead, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if detail.PDFObjectKey != nil && *detail.PDFObjectKey != "" && h.storage != nil {
		url, err := h.storage.PresignGet(ctx, *detail.PDFObjectKey, 15*time.Minute)
		if err == nil {
			return gen.GetPrescriptionPDF200JSONResponse{
				Status: gen.GetPrescriptionPDF200JSONResponseBodyStatusReady,
				Url:    url,
			}, nil
		}
	}

	return gen.GetPrescriptionPDF200JSONResponse{
		Status: gen.GetPrescriptionPDF200JSONResponseBodyStatusPending,
		Url:    "",
	}, nil
}

// ListEncounterPrescriptions lista recetas derivadas de un encuentro clínico firmado (PP-F4-API-002).
func (h *PrescriptionHandlers) ListEncounterPrescriptions(ctx context.Context, req gen.ListEncounterPrescriptionsRequestObject) (gen.ListEncounterPrescriptionsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	var res authz.EncounterResource
	if h.encounterReader != nil {
		encRes, err := h.encounterReader.GetEncounterAccess(ctx, req.EncounterId)
		if err != nil {
			return nil, err
		}
		res = *encRes
	} else {
		res = authz.EncounterResource{ID: req.EncounterId}
	}

	d := authz.Can(p, authz.EncounterRead, res)
	if !d.Allow {
		if !authz.EncounterVisible(p, res) {
			return nil, &kernel.NotFoundError{Resource: "encounter"}
		}
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	summaries, err := h.svc.ListByEncounter(ctx, req.EncounterId)
	if err != nil {
		return nil, err
	}

	data := make([]gen.PrescriptionSummary, 0, len(summaries))
	for _, s := range summaries {
		data = append(data, toGenSummary(s))
	}

	return gen.ListEncounterPrescriptions200JSONResponse{Data: data}, nil
}

// ListMyPrescriptions lista las recetas de las mascotas del tutor actual (PP-F4-API-004).
func (h *PrescriptionHandlers) ListMyPrescriptions(ctx context.Context, req gen.ListMyPrescriptionsRequestObject) (gen.ListMyPrescriptionsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = *req.Params.Status
	}
	source := ""
	if req.Params.Source != nil {
		source = string(*req.Params.Source)
	}
	limit := limitOf(req.Params.Limit)

	summaries, err := h.svc.ListByGuardian(ctx, p.UserID, req.Params.PetId, status, source, limit)
	if err != nil {
		return nil, err
	}

	data := make([]gen.PrescriptionSummary, 0, len(summaries))
	for _, s := range summaries {
		data = append(data, toGenSummary(s))
	}

	return gen.ListMyPrescriptions200JSONResponse{Data: data}, nil
}

// ArchivePrescription oculta la receta de la lista activa del tutor (PP-F4-API-004).
func (h *PrescriptionHandlers) ArchivePrescription(ctx context.Context, req gen.ArchivePrescriptionRequestObject) (gen.ArchivePrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if err := h.svc.Archive(ctx, req.PrescriptionId, p.UserID); err != nil {
		return nil, err
	}

	return gen.ArchivePrescription200JSONResponse{Success: true}, nil
}

// VerifyPrescription efectúa la verificación pública contra el hash canónico (PP-F4-API-004, SEC-006).
func (h *PrescriptionHandlers) VerifyPrescription(ctx context.Context, req gen.VerifyPrescriptionRequestObject) (gen.VerifyPrescriptionResponseObject, error) {
	hParam := ""
	if req.Params.H != nil {
		hParam = *req.Params.H
	}

	info := reqctx.From(ctx)
	ipAddr := info.RemoteAddr

	res, err := h.svc.PublicVerify(ctx, req.PrescriptionId, hParam, ipAddr)
	if err != nil {
		return nil, err
	}

	var issuedAt *time.Time
	if res.IssuedAt != nil {
		issuedAt = res.IssuedAt
	}

	var validUntilDate *openapi_types.Date
	if res.ValidUntil != nil && *res.ValidUntil != "" {
		if t, err := time.Parse("2006-01-02", *res.ValidUntil); err == nil {
			d := openapi_types.Date{Time: t}
			validUntilDate = &d
		}
	}

	items := make([]gen.PrescriptionItemDTO, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, toGenPrescriptionItem(it))
	}

	return gen.VerifyPrescription200JSONResponse{
		PrescriptionId:     res.PrescriptionID,
		Valid:              res.Valid,
		Status:             res.Status,
		ContentHash:        res.ContentHash,
		VerifiedAt:         res.VerifiedAt,
		Country:            res.Country,
		PetName:            &res.PetName,
		PetSpecies:         &res.PetSpecies,
		IssuerName:         &res.IssuerName,
		IssuerOrgName:      &res.IssuerOrgName,
		VetRegistry:        &res.VetRegistry,
		IssuedAt:           issuedAt,
		ValidUntil:         validUntilDate,
		RefillsAllowed:     res.RefillsAllowed,
		RefillsUsed:        res.RefillsUsed,
		Items:              &items,
		DispensationsCount: res.DispensationsCount,
		ErrorReason:        &res.ErrorReason,
		RevocationReason:   &res.RevocationReason,
	}, nil
}

func toDomainItem(it gen.PrescriptionItemDTO) domain.PrescriptionItem {
	var catID *uuid.UUID
	if it.CatalogId != nil {
		catID = it.CatalogId
	}
	name := strings.TrimSpace(it.Name)
	var freeText *string
	if it.FreeText != nil {
		freeText = it.FreeText
	} else if catID == nil {
		freeText = &name
	}

	pres := ""
	if it.Presentation != nil {
		pres = *it.Presentation
	}
	instr := ""
	if it.Instructions != nil {
		instr = *it.Instructions
	}
	subAllowed := true
	if it.SubstitutionAllowed != nil {
		subAllowed = *it.SubstitutionAllowed
	}
	ctrl := false
	if it.Controlled != nil {
		ctrl = *it.Controlled
	}
	itemID := it.ItemId
	if itemID == nil || *itemID == "" {
		id := uuid.New().String()
		itemID = &id
	}

	var extRef *domain.ExtractionRefVO
	if it.ExtractionRef != nil {
		extRef = &domain.ExtractionRefVO{
			ItemIndex: *it.ExtractionRef,
		}
	}

	return domain.PrescriptionItem{
		ItemID:       *itemID,
		CatalogID:    catID,
		FreeText:     freeText,
		NameSnapshot: name,
		Presentation: pres,
		Dose: domain.DoseVO{
			Value: float64(it.Dose.Value),
			Unit:  it.Dose.Unit,
		},
		Route:        it.Route,
		Frequency:    it.Frequency,
		DurationDays: it.DurationDays,
		QuantityToDispense: domain.QuantityVO{
			Value: float64(it.Quantity.Value),
			Unit:  it.Quantity.Unit,
		},
		SubstitutionAllowed: subAllowed,
		Instructions:        instr,
		Controlled:          ctrl,
		ExtractionRef:       extRef,
	}
}

func toGenPrescriptionItem(it domain.PrescriptionItem) gen.PrescriptionItemDTO {
	subAllowed := it.SubstitutionAllowed
	ctrl := it.Controlled
	pres := it.Presentation
	instr := it.Instructions
	itemID := it.ItemID
	var extRef *int
	if it.ExtractionRef != nil {
		idx := it.ExtractionRef.ItemIndex
		extRef = &idx
	}
	return gen.PrescriptionItemDTO{
		ItemId:       &itemID,
		CatalogId:    it.CatalogID,
		FreeText:     it.FreeText,
		Name:         it.NameSnapshot,
		Presentation: &pres,
		Dose: gen.PrescriptionDose{
			Value: float32(it.Dose.Value),
			Unit:  it.Dose.Unit,
		},
		Route:        it.Route,
		Frequency:    it.Frequency,
		DurationDays: it.DurationDays,
		Quantity: gen.PrescriptionQuantity{
			Value: float32(it.QuantityToDispense.Value),
			Unit:  it.QuantityToDispense.Unit,
		},
		SubstitutionAllowed: &subAllowed,
		Instructions:        &instr,
		Controlled:          &ctrl,
		ExtractionRef:       extRef,
	}
}

func toGenSummary(s app.ReadPrescriptionSummary) gen.PrescriptionSummary {
	var validUntil *openapi_types.Date
	if s.ValidUntil != nil {
		if t, err := time.Parse("2006-01-02", *s.ValidUntil); err == nil {
			d := openapi_types.Date{Time: t}
			validUntil = &d
		}
	}
	return gen.PrescriptionSummary{
		Id:                 s.ID,
		Version:            int(s.Version),
		PetId:              s.PetID,
		GuardianId:         s.GuardianID,
		Country:            s.Country,
		Source:             gen.PrescriptionSummarySource(s.Source),
		Status:             gen.PrescriptionSummaryStatus(s.Status),
		IssuerId:           s.IssuerID,
		IssuerOrgId:        s.IssuerOrgID,
		IssuedAt:           s.IssuedAt,
		ValidUntil:         validUntil,
		RefillsAllowed:     s.RefillsAllowed,
		RefillsUsed:        s.RefillsUsed,
		ItemsCount:         s.ItemsCount,
		HasControlled:      s.HasControlled,
		PdfObjectKey:       s.PDFObjectKey,
		ArchivedByGuardian: s.ArchivedByGuardian,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}
}

func toGenPrescriptionDetail(d *app.ReadPrescriptionDetail) gen.PrescriptionDetail {
	items := make([]gen.PrescriptionItemDTO, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, toGenPrescriptionItem(it))
	}

	var validUntil *openapi_types.Date
	if d.ValidUntil != nil {
		if t, err := time.Parse("2006-01-02", *d.ValidUntil); err == nil {
			dVal := openapi_types.Date{Time: t}
			validUntil = &dVal
		}
	}

	var credDTO *struct {
		Country        *string    `json:"country,omitempty"`
		RegistryNumber *string    `json:"registry_number,omitempty"`
		VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	}
	if d.Credential != nil {
		c := d.Credential.Country
		r := d.Credential.RegistryNumber
		vt := d.Credential.VerifiedAt
		credDTO = &struct {
			Country        *string    `json:"country,omitempty"`
			RegistryNumber *string    `json:"registry_number,omitempty"`
			VerifiedAt     *time.Time `json:"verified_at,omitempty"`
		}{
			Country:        &c,
			RegistryNumber: &r,
			VerifiedAt:     &vt,
		}
	}

	var sigDTO *struct {
		ContentHash   *string    `json:"content_hash,omitempty"`
		SignedAt      *time.Time `json:"signed_at,omitempty"`
		SignedBy      *uuid.UUID `json:"signed_by,omitempty"`
		StreamVersion *int       `json:"stream_version,omitempty"`
	}
	if d.Signature != nil {
		ch := d.Signature.ContentHash
		sa := d.Signature.SignedAt
		sb := d.Signature.SignedBy
		sv := int(d.Signature.StreamVersion)
		sigDTO = &struct {
			ContentHash   *string    `json:"content_hash,omitempty"`
			SignedAt      *time.Time `json:"signed_at,omitempty"`
			SignedBy      *uuid.UUID `json:"signed_by,omitempty"`
			StreamVersion *int       `json:"stream_version,omitempty"`
		}{
			ContentHash:   &ch,
			SignedAt:      &sa,
			SignedBy:      &sb,
			StreamVersion: &sv,
		}
	}

	var remaining *map[string]float32
	if d.ItemsRemaining != nil {
		m := make(map[string]float32, len(d.ItemsRemaining))
		for k, v := range d.ItemsRemaining {
			m[k] = float32(v)
		}
		remaining = &m
	}

	var images *[]gen.PrescriptionImageDTO
	if len(d.Images) > 0 {
		imgList := make([]gen.PrescriptionImageDTO, 0, len(d.Images))
		for _, img := range d.Images {
			ct := img.ContentType
			imgList = append(imgList, gen.PrescriptionImageDTO{
				Page:        img.Page,
				ObjectKey:   img.ObjectKey,
				ContentType: &ct,
			})
		}
		images = &imgList
	}

	var extMeta *struct {
		ClinicName *string `json:"clinic_name,omitempty"`
		IssuedOn   *string `json:"issued_on,omitempty"`
		VetName    *string `json:"vet_name,omitempty"`
	}
	if d.ExternalMeta != nil {
		var issuedOnStr *string
		if d.ExternalMeta.IssuedOn != nil {
			s := d.ExternalMeta.IssuedOn.String()
			issuedOnStr = &s
		}
		extMeta = &struct {
			ClinicName *string `json:"clinic_name,omitempty"`
			IssuedOn   *string `json:"issued_on,omitempty"`
			VetName    *string `json:"vet_name,omitempty"`
		}{
			ClinicName: d.ExternalMeta.ClinicName,
			IssuedOn:   issuedOnStr,
			VetName:    d.ExternalMeta.VetName,
		}
	}

	return gen.PrescriptionDetail{
		Id:                 d.ID,
		Version:            int(d.Version),
		PetId:              d.PetID,
		GuardianId:         d.GuardianID,
		Country:            d.Country,
		Source:             gen.PrescriptionDetailSource(d.Source),
		Status:             gen.PrescriptionDetailStatus(d.Status),
		IssuerId:           d.IssuerID,
		IssuerOrgId:        d.IssuerOrgID,
		EncounterId:        d.EncounterID,
		IssuedAt:           d.IssuedAt,
		ValidUntil:         validUntil,
		RefillsAllowed:     d.RefillsAllowed,
		RefillsUsed:        d.RefillsUsed,
		Items:              items,
		ItemsRemaining:     remaining,
		HasControlled:      d.HasControlled,
		PdfObjectKey:       d.PDFObjectKey,
		Credential:         credDTO,
		Signature:          sigDTO,
		VerificationsCount: d.Verifications,
		Notes:              d.Notes,
		ArchivedByGuardian: d.ArchivedByGuardian,
		Images:             images,
		ExternalMeta:       extMeta,
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
	}
}

func limitOf(l *int) int {
	if l == nil || *l <= 0 {
		return 20
	}
	if *l > 100 {
		return 100
	}
	return *l
}

// UploadExternalPrescription sube entre 1 y 5 imágenes de receta externa e inicia la extracción OCR (PP-F4-API-003, PRD-005).
func (h *PrescriptionHandlers) UploadExternalPrescription(ctx context.Context, req gen.UploadExternalPrescriptionRequestObject) (gen.UploadExternalPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	petID := uuid.UUID(req.PetId)
	if h.petReader == nil {
		return nil, errors.New("prescriptions: petReader no configurado")
	}
	_, _, guardianID, err := h.petReader.GetPetInfo(ctx, petID)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PrescriptionUpload, authz.PetResource{ID: petID, GuardianID: guardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil {
		return nil, kernel.NewDomainError("EMPTY_IMAGES", "Se requiere al menos una imagen de la receta.")
	}

	var images []domain.ImageRef
	var country string
	page := 1

	for {
		part, err := req.Body.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		formName := part.FormName()
		if formName == "country" {
			cBytes, _ := io.ReadAll(io.LimitReader(part, 16))
			country = strings.ToUpper(strings.TrimSpace(string(cBytes)))
			continue
		}
		if part.FileName() == "" && formName != "files" && formName != "images" && formName != "file" {
			continue
		}
		if len(images) >= 5 {
			return nil, kernel.NewDomainError("EXTERNAL_UPLOAD_LIMIT", "No se pueden subir más de 5 páginas por receta.")
		}
		data, err := io.ReadAll(io.LimitReader(part, 10*1024*1024+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 10*1024*1024 {
			return nil, kernel.NewDomainError("PAYLOAD_TOO_LARGE", "Cada página no puede superar 10 MB.")
		}
		if len(data) == 0 {
			continue
		}

		ct := http.DetectContentType(data)
		ext := ".jpg"
		if strings.Contains(ct, "pdf") {
			ext = ".pdf"
			ct = "application/pdf"
		} else if strings.Contains(ct, "png") {
			ext = ".png"
			ct = "image/png"
		} else {
			ct = "image/jpeg"
		}

		imgID := uuid.New().String()
		key := fmt.Sprintf("prescriptions/external/%s/%d_%s%s", petID.String(), page, imgID, ext)
		if h.storage != nil {
			if err := h.storage.Put(ctx, key, ct, bytes.NewReader(data), int64(len(data))); err != nil {
				return nil, err
			}
		}
		images = append(images, domain.ImageRef{
			Page:        page,
			ObjectKey:   key,
			ContentType: ct,
		})
		page++
	}

	if len(images) == 0 {
		return nil, kernel.NewDomainError("EMPTY_IMAGES", "Se requiere al menos una imagen de la receta.")
	}
	if country == "" {
		country = "CO"
	}

	cmd := app.UploadExternalPrescriptionCmd{
		PetID:   petID,
		Country: country,
		Images:  images,
	}
	rxID, err := h.svc.UploadExternalPrescription(ctx, cmd, guardianID)
	if err != nil {
		return nil, err
	}

	if h.enqueuer != nil {
		_, _ = h.enqueuer.EnqueueOCRPipeline(ctx, queue.OCRPipelinePayload{
			PrescriptionID: rxID,
			Attempt:        1,
			Country:        country,
		})
	}

	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}

	return gen.UploadExternalPrescription202JSONResponse(toGenPrescriptionDetail(detail)), nil
}

// AddPrescriptionImage añade una página adicional a una receta en borrador (PP-F4-API-003).
func (h *PrescriptionHandlers) AddPrescriptionImage(ctx context.Context, req gen.AddPrescriptionImageRequestObject) (gen.AddPrescriptionImageResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionUpload, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil {
		return nil, kernel.NewDomainError("EMPTY_IMAGES", "Falta el archivo de la página.")
	}

	part, err := req.Body.NextPart()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(part, 10*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 10*1024*1024 {
		return nil, kernel.NewDomainError("PAYLOAD_TOO_LARGE", "La página no puede superar 10 MB.")
	}
	if len(data) == 0 {
		return nil, kernel.NewDomainError("EMPTY_IMAGES", "Archivo de imagen vacío.")
	}

	ct := http.DetectContentType(data)
	ext := ".jpg"
	if strings.Contains(ct, "pdf") {
		ext = ".pdf"
		ct = "application/pdf"
	} else if strings.Contains(ct, "png") {
		ext = ".png"
		ct = "image/png"
	} else {
		ct = "image/jpeg"
	}

	page := len(detail.Images) + 1
	imgID := uuid.New().String()
	key := fmt.Sprintf("prescriptions/external/%s/%d_%s%s", detail.PetID.String(), page, imgID, ext)
	if h.storage != nil {
		if err := h.storage.Put(ctx, key, ct, bytes.NewReader(data), int64(len(data))); err != nil {
			return nil, err
		}
	}

	imgRef := domain.ImageRef{
		Page:        page,
		ObjectKey:   key,
		ContentType: ct,
	}
	guardianID := p.UserID
	if p.HasRole("super_admin") {
		guardianID = detail.GuardianID
	}
	if err := h.svc.AddPrescriptionImage(ctx, rxID, imgRef, guardianID); err != nil {
		return nil, err
	}

	return gen.AddPrescriptionImage201JSONResponse{
		ObjectKey: key,
		Page:      page,
	}, nil
}

// GetPrescriptionImage devuelve la URL de lectura o redirección prefirmada de una página (PP-F4-API-003).
func (h *PrescriptionHandlers) GetPrescriptionImage(ctx context.Context, req gen.GetPrescriptionImageRequestObject) (gen.GetPrescriptionImageResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionRead, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	img, err := h.svc.GetImage(ctx, rxID, req.Page)
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, &kernel.NotFoundError{Resource: "image"}
	}

	if h.storage == nil {
		return nil, errors.New("prescriptions: storage no configurado")
	}
	url, err := h.storage.PresignGet(ctx, img.ObjectKey, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	return gen.GetPrescriptionImage200JSONResponse{
		Url: url,
	}, nil
}

// GetPrescriptionExtraction consulta el estado y datos del análisis OCR (PP-F4-API-003).
func (h *PrescriptionHandlers) GetPrescriptionExtraction(ctx context.Context, req gen.GetPrescriptionExtractionRequestObject) (gen.GetPrescriptionExtractionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionRead, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	job, err := h.svc.GetExtraction(ctx, rxID)
	if err != nil {
		return nil, err
	}

	return gen.GetPrescriptionExtraction200JSONResponse(toGenExtractionResponse(job)), nil
}

// RetryPrescriptionExtraction solicita un reintento del análisis OCR si falló o requirió reupload (PP-F4-API-003).
func (h *PrescriptionHandlers) RetryPrescriptionExtraction(ctx context.Context, req gen.RetryPrescriptionExtractionRequestObject) (gen.RetryPrescriptionExtractionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionUpload, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	guardianID := p.UserID
	if p.HasRole("super_admin") {
		guardianID = detail.GuardianID
	}
	if err := h.svc.RetryExtraction(ctx, rxID, guardianID); err != nil {
		return nil, err
	}

	job, err := h.svc.GetExtraction(ctx, rxID)
	if err != nil {
		return nil, err
	}

	if h.enqueuer != nil {
		_, _ = h.enqueuer.EnqueueOCRPipeline(ctx, queue.OCRPipelinePayload{
			PrescriptionID: rxID,
			Attempt:        job.Attempt,
			Country:        detail.Country,
		})
	}

	return gen.RetryPrescriptionExtraction202JSONResponse(toGenExtractionResponse(job)), nil
}

// ConfirmExternalPrescription confirma los datos revisados con atestación humana explícita (PP-F4-API-003, PRD-006).
func (h *PrescriptionHandlers) ConfirmExternalPrescription(ctx context.Context, req gen.ConfirmExternalPrescriptionRequestObject) (gen.ConfirmExternalPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionUpload, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	body := req.Body
	if body == nil {
		return nil, &kernel.ValidationError{}
	}
	if !body.Attestation {
		return nil, kernel.NewDomainError("ATTESTATION_REQUIRED", "Debes confirmar que los medicamentos coinciden con tu receta.")
	}

	var items []domain.PrescriptionItem
	for _, it := range body.Items {
		items = append(items, toDomainItem(it))
	}

	var val domain.Validity
	if body.Validity != nil {
		if body.Validity.ValidUntil != nil {
			vu, err := kernel.NewCivilDate(body.Validity.ValidUntil.String())
			if err == nil {
				val.ValidUntil = vu
			}
		}
		if body.Validity.RefillsAllowed != nil {
			val.RefillsAllowed = *body.Validity.RefillsAllowed
		}
	}

	var meta domain.ExternalMeta
	if body.ExternalMeta != nil {
		meta.VetName = body.ExternalMeta.VetName
		meta.ClinicName = body.ExternalMeta.ClinicName
		if body.ExternalMeta.IssuedOn != nil {
			if vu, err := kernel.NewCivilDate(*body.ExternalMeta.IssuedOn); err == nil {
				meta.IssuedOn = &vu
			}
		}
	}
	var notes string
	if body.Notes != nil {
		notes = *body.Notes
	}

	guardianID := p.UserID
	if p.HasRole("super_admin") {
		guardianID = detail.GuardianID
	}
	cmd := app.ConfirmExternalPrescriptionCmd{
		PrescriptionID: rxID,
		Items:          items,
		Validity:       val,
		ExternalMeta:   meta,
		Notes:          notes,
		Attestation:    body.Attestation,
	}
	if err := h.svc.ConfirmExternalPrescription(ctx, cmd, guardianID); err != nil {
		return nil, err
	}

	updated, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	ver := updated.Version
	return gen.ConfirmExternalPrescription200JSONResponse{
		Body: toGenPrescriptionDetail(updated),
		Headers: gen.ConfirmExternalPrescription200ResponseHeaders{
			ETag: &ver,
		},
	}, nil
}

// ReopenExternalPrescription reabre una receta externa confirmada a borrador (PP-F4-API-003, DOM-007).
func (h *PrescriptionHandlers) ReopenExternalPrescription(ctx context.Context, req gen.ReopenExternalPrescriptionRequestObject) (gen.ReopenExternalPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	rxID := uuid.UUID(req.PrescriptionId)
	detail, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}
	res := authz.PrescriptionResource{
		ID:          detail.ID,
		PetID:       detail.PetID,
		GuardianID:  detail.GuardianID,
		IssuerOrgID: detail.IssuerOrgID,
		IssuerID:    detail.IssuerID,
	}
	if d := authz.Can(p, authz.PrescriptionUpload, res); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	guardianID := p.UserID
	if p.HasRole("super_admin") {
		guardianID = detail.GuardianID
	}
	if err := h.svc.ReopenExternal(ctx, rxID, guardianID); err != nil {
		return nil, err
	}

	updated, err := h.svc.GetByID(ctx, rxID)
	if err != nil {
		return nil, err
	}

	return gen.ReopenExternalPrescription200JSONResponse(toGenPrescriptionDetail(updated)), nil
}

func toGenExtractionResponse(job *app.ExtractionJobDTO) gen.ExtractionResponse {
	resp := gen.ExtractionResponse{
		Attempt: job.Attempt,
		Reason:  job.Reason,
	}

	switch job.Status {
	case "queued", "preprocessing", "extracting", "matching":
		resp.Status = gen.ExtractionResponseStatusQueued
	case "done":
		resp.Status = gen.ExtractionResponseStatusDone
	case "reupload_requested":
		resp.Status = gen.ExtractionResponseStatusReuploadRequested
	case "failed", "refused":
		resp.Status = gen.ExtractionResponseStatusFailed
	default:
		resp.Status = gen.ExtractionResponseStatusQueued
	}

	if job.Extraction != nil {
		ext := job.Extraction
		data := gen.ExtractionDataDTO{
			Warnings: &ext.Warnings,
		}
		if ext.Legibility > 0 {
			leg := fmt.Sprintf("%.2f", ext.Legibility)
			data.Legibility = &leg
		}
		hdr := gen.ExtractionHeaderDTO{
			VetName:    ext.Header.VetName,
			ClinicName: ext.Header.Clinic,
			IssuedOn:   ext.Header.Date,
		}
		data.Header = &hdr

		items := make([]gen.ExtractionItemDTO, 0, len(ext.Items))
		for _, it := range ext.Items {
			itDTO := gen.ExtractionItemDTO{
				Index:   it.Index,
				RawText: it.RawText,
			}
			if it.Dose != "" {
				itDTO.Dose = &it.Dose
			}
			if it.Frequency != "" {
				itDTO.Frequency = &it.Frequency
			}
			if it.Duration != "" {
				itDTO.Duration = &it.Duration
			}
			if it.Quantity != "" {
				itDTO.Quantity = &it.Quantity
			}

			// product
			prodDTO := struct {
				Candidates *[]gen.ExtractionCandidateDTO `json:"candidates,omitempty"`
				Text       *string                       `json:"text,omitempty"`
			}{
				Text: &it.Product.Text,
			}
			if len(it.Product.Candidates) > 0 {
				candDTOs := make([]gen.ExtractionCandidateDTO, 0, len(it.Product.Candidates))
				for _, c := range it.Product.Candidates {
					candDTOs = append(candDTOs, gen.ExtractionCandidateDTO{
						CatalogId: c.CatalogID,
						Name:      c.Name,
						Score:     float32(c.Score),
					})
				}
				prodDTO.Candidates = &candDTOs
			}
			itDTO.Product = &prodDTO

			// confidence
			if len(it.Confidence) > 0 {
				conf := gen.ExtractionConfidenceDTO{}
				if v, ok := it.Confidence["product"]; ok {
					f := float32(v)
					conf.Product = &f
				}
				if v, ok := it.Confidence["dose"]; ok {
					f := float32(v)
					conf.Dose = &f
				}
				if v, ok := it.Confidence["frequency"]; ok {
					f := float32(v)
					conf.Frequency = &f
				}
				if v, ok := it.Confidence["duration"]; ok {
					f := float32(v)
					conf.Duration = &f
				}
				if v, ok := it.Confidence["quantity"]; ok {
					f := float32(v)
					conf.Quantity = &f
				}
				if v, ok := it.Confidence["overall"]; ok {
					f := float32(v)
					conf.Overall = &f
				}
				itDTO.Confidence = &conf
			}

			if len(it.BBox) > 0 {
				bbox := make([]float32, len(it.BBox))
				for i, b := range it.BBox {
					bbox[i] = float32(b)
				}
				itDTO.Bbox = &bbox
			}

			items = append(items, itDTO)
		}
		data.Items = &items
		resp.Extraction = &data
	}

	return resp
}
