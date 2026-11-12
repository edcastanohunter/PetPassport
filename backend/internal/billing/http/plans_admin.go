package http

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Versionado de planes (PP-F5-API-001): PUT crea un draft, publish lo hace
// vigente con preaviso de 30 días si hay suscripciones en otra versión
// (RN-39) y retire lo saca del catálogo (B10: las suscripciones conservan su
// versión).

// planNoticeDays es el preaviso mínimo de una nueva versión con suscripciones vivas (RN-39).
const planNoticeDays = 30

// planTarget identifica una versión de plan en el audit log (ref.plans no tiene uuid).
func planTarget(code string, version int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("plan:"+code+":"+strconv.Itoa(version)))
}

func mapPlanError(err error) error {
	if errors.Is(err, infra.ErrPlanVersionState) {
		return kernel.NewDomainError("PLAN_VERSION_STATE", "La versión del plan no está en el estado que requiere la operación.")
	}
	return err
}

// AdminListPlanVersions lista todas las versiones de los planes.
func (h *BillingHandlers) AdminListPlanVersions(ctx context.Context, _ gen.AdminListPlanVersionsRequestObject) (gen.AdminListPlanVersionsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminPlansWrite, nil); err != nil {
		return nil, err
	}
	plans, err := h.queries.ListPlanVersions(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.AdminListPlanVersions200JSONResponse{Data: make([]gen.BillingPlan, 0, len(plans))}
	for _, p := range plans {
		out.Data = append(out.Data, toGenPlan(p))
	}
	return out, nil
}

// PublishPlan publica una versión draft desde effective_from.
func (h *BillingHandlers) PublishPlan(ctx context.Context, req gen.PublishPlanRequestObject) (gen.PublishPlanResponseObject, error) {
	p, err := h.authorize(ctx, authz.AdminPlansWrite, nil)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "version", Code: "REQUIRED"}}}
	}
	body := *req.Body
	plan, err := h.queries.PlanVersion(ctx, req.Code, body.Version)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, &kernel.NotFoundError{Resource: "Plan " + req.Code}
	}
	effective := body.EffectiveFrom.Time
	live, err := h.queries.LiveSubscriptionsOnOtherVersions(ctx, req.Code, body.Version)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if live > 0 && effective.Before(today.AddDate(0, 0, planNoticeDays)) {
		return nil, kernel.NewDomainError("PLAN_NOTICE_TOO_SHORT",
			"Hay suscripciones vigentes en otra versión: la nueva debe entrar en vigor con al menos 30 días de preaviso.")
	}
	details := map[string]any{"code": req.Code, "version": body.Version, "effective_from": effective.Format(time.DateOnly), "live_subscriptions": live}
	if err := h.adminWrite(ctx, "plan.publish", "plan", planTarget(req.Code, body.Version), body.Reason, details); err != nil {
		return nil, err
	}
	if err := h.queries.PublishPlanVersion(ctx, req.Code, body.Version, effective, p.UserID); err != nil {
		return nil, mapPlanError(err)
	}
	plan.Status = "published"
	plan.EffectiveFrom = &effective
	return gen.PublishPlan200JSONResponse(toGenPlan(*plan)), nil
}

// RetirePlan saca del catálogo una versión publicada.
func (h *BillingHandlers) RetirePlan(ctx context.Context, req gen.RetirePlanRequestObject) (gen.RetirePlanResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminPlansWrite, nil); err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "version", Code: "REQUIRED"}}}
	}
	body := *req.Body
	plan, err := h.queries.PlanVersion(ctx, req.Code, body.Version)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, &kernel.NotFoundError{Resource: "Plan " + req.Code}
	}
	details := map[string]any{"code": req.Code, "version": body.Version}
	if err := h.adminWrite(ctx, "plan.retire", "plan", planTarget(req.Code, body.Version), strings.TrimSpace(body.Reason), details); err != nil {
		return nil, err
	}
	if err := h.queries.RetirePlanVersion(ctx, req.Code, body.Version); err != nil {
		return nil, mapPlanError(err)
	}
	plan.Status = "retired"
	return gen.RetirePlan200JSONResponse(toGenPlan(*plan)), nil
}
