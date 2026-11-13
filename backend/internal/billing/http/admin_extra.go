package http

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Retirada del método de pago, cambio de plan administrativo y cupones
// (PP-F5-API-004, API-007, PRD-022).

// RemovePaymentMethod retira el método de pago de la cuenta.
func (h *BillingHandlers) RemovePaymentMethod(ctx context.Context, req gen.RemovePaymentMethodRequestObject) (gen.RemovePaymentMethodResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	if h.svc == nil {
		return gen.RemovePaymentMethod204Response{}, nil
	}
	if err := h.svc.RemovePaymentMethod(ctx, req.Id); err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.RemovePaymentMethod204Response{}, nil
}

// adminChangePlan cambia el plan de una suscripción en nombre del cliente con
// las mismas reglas que el cambio que hace el propio cliente (prorrateo, B3).
func (h *BillingHandlers) adminChangePlan(ctx context.Context, subID uuid.UUID, body gen.AdminAdjustSubscriptionRequest) error {
	if body.PlanCode == nil || strings.TrimSpace(*body.PlanCode) == "" {
		return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "plan_code", Code: "REQUIRED"}}}
	}
	if h.svc == nil {
		return nil
	}
	current, err := h.queries.GetSubscription(ctx, subID)
	if err != nil {
		return err
	}
	if current == nil {
		return &kernel.NotFoundError{Resource: "Suscripción"}
	}
	cycle := current.Cycle
	if body.Cycle != nil {
		cycle = domain.BillingCycle(*body.Cycle)
	}
	return mapPaymentError(h.svc.ChangePlan(ctx, subID, strings.TrimSpace(*body.PlanCode), body.Seats, current.Tier, cycle))
}

func toGenCoupon(c infra.Coupon) gen.Coupon {
	out := gen.Coupon{
		Code: c.Code, Kind: gen.CouponKind(c.Kind), Value: float32(c.Value), Uses: c.Uses, Active: c.Active,
		DurationMonths: c.DurationMonths, MaxUses: c.MaxUses,
	}
	if c.ValidUntil != nil {
		out.ValidUntil = &openapi_types.Date{Time: *c.ValidUntil}
	}
	if len(c.Plans) > 0 {
		plans := c.Plans
		out.Plans = &plans
	}
	return out
}

// AdminListCoupons lista el catálogo de cupones.
func (h *BillingHandlers) AdminListCoupons(ctx context.Context, req gen.AdminListCouponsRequestObject) (gen.AdminListCouponsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingRead, nil); err != nil {
		return nil, err
	}
	rows, err := h.coupons.List(ctx, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	data := make([]gen.Coupon, 0, len(rows))
	for _, c := range rows {
		data = append(data, toGenCoupon(c))
	}
	return gen.AdminListCoupons200JSONResponse{Data: data}, nil
}

// AdminCreateCoupon da de alta un cupón.
func (h *BillingHandlers) AdminCreateCoupon(ctx context.Context, req gen.AdminCreateCouponRequestObject) (gen.AdminCreateCouponResponseObject, error) {
	body := gen.AdminCreateCouponJSONRequestBody{}
	if req.Body != nil {
		body = *req.Body
	}
	c := infra.Coupon{
		Code: strings.ToUpper(strings.TrimSpace(body.Code)), Kind: string(body.Kind), Value: float64(body.Value),
		DurationMonths: body.DurationMonths, MaxUses: body.MaxUses, Active: true,
	}
	if body.ValidUntil != nil {
		t := body.ValidUntil.Time
		c.ValidUntil = &t
	}
	if body.Plans != nil {
		c.Plans = *body.Plans
	}
	details := map[string]any{"code": c.Code, "kind": c.Kind, "value": c.Value}
	// El cupón no tiene uuid: el audit log lo identifica por el código en Details.
	if err := h.adminWrite(ctx, "coupon.create", "billing_coupon", uuid.NewSHA1(uuid.NameSpaceOID, []byte("coupon:"+c.Code)), body.Reason, details); err != nil {
		return nil, err
	}
	var violations []kernel.FieldViolation
	if c.Code == "" {
		violations = append(violations, kernel.FieldViolation{Field: "code", Code: "REQUIRED"})
	}
	if c.Kind == "pct" && (c.Value <= 0 || c.Value > 100) || c.Kind == "amount" && c.Value <= 0 || c.Kind != "pct" && c.Kind != "amount" {
		violations = append(violations, kernel.FieldViolation{Field: "value", Code: "INVALID"})
	}
	if c.ValidUntil != nil && c.ValidUntil.Before(time.Now().UTC().Truncate(24*time.Hour)) {
		violations = append(violations, kernel.FieldViolation{Field: "valid_until", Code: "IN_PAST"})
	}
	if len(violations) > 0 {
		return nil, &kernel.ValidationError{Fields: violations}
	}
	if err := h.coupons.Create(ctx, c); err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.AdminCreateCoupon201JSONResponse(toGenCoupon(c)), nil
}
