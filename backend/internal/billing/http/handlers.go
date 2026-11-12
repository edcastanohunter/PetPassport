package http

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// BillingHandlers implementa los endpoints de facturación del contrato OpenAPI (PP-F5-API-001..003, 006).
type BillingHandlers struct {
	svc     *app.BillingService
	queries *infra.Queries
	logger  *slog.Logger

	// Cobro y facturación (M5.2, WithPayments). Los almacenes toleran receptor nil.
	pay      *app.PaymentService
	inv      *app.InvoicingService
	ledger   *infra.Ledger
	invoices *infra.InvoiceStore
	objects  Presigner
	scope    Scope
}

// NewBillingHandlers crea una nueva instancia de BillingHandlers.
func NewBillingHandlers(svc *app.BillingService, queries *infra.Queries, logger *slog.Logger) *BillingHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &BillingHandlers{
		svc:     svc,
		queries: queries,
		logger:  logger,
		scope:   queryScope{queries: queries},
	}
}

func (h *BillingHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "unauthenticated"}
	}
	return p, nil
}

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrPaymentMethodRequired):
		return &kernel.DomainError{Code: "PAYMENT_METHOD_REQUIRED", Message: err.Error()}
	case errors.Is(err, domain.ErrSeatsBelowUsage):
		return &kernel.DomainError{Code: "SEATS_BELOW_USAGE", Message: err.Error()}
	case errors.Is(err, domain.ErrPlanNotAvailable):
		return &kernel.DomainError{Code: "PLAN_NOT_AVAILABLE", Message: err.Error()}
	case errors.Is(err, domain.ErrPlanChangePending):
		return &kernel.DomainError{Code: "PLAN_CHANGE_PENDING", Message: err.Error()}
	case errors.Is(err, domain.ErrInvalidTaxID):
		return &kernel.DomainError{Code: "TAX_ID_INVALID", Message: err.Error()}
	case errors.Is(err, domain.ErrLegalEntityIncomplete):
		return &kernel.DomainError{Code: "LEGAL_ENTITY_INCOMPLETE", Message: err.Error()}
	case errors.Is(err, domain.ErrOrgAlreadyBilled):
		return &kernel.DomainError{Code: "ORGANIZATION_ALREADY_BILLED", Message: err.Error()}
	case errors.Is(err, domain.ErrActiveSubscription):
		return &kernel.DomainError{Code: "CONFLICT_VERSION", Message: "La cuenta tiene suscripciones activas"}
	default:
		return err
	}
}

func toGenPlan(p domain.PlanDefinition) gen.BillingPlan {
	quotas := make(map[string]int, len(p.Quotas))
	for k, v := range p.Quotas {
		quotas[k] = int(v)
	}

	tiers := make([]gen.VolumeTier, 0, len(p.Pricing.Tiers))
	for _, t := range p.Pricing.Tiers {
		tiers = append(tiers, gen.VolumeTier{
			UpTo:         t.UpTo,
			MonthlyPrice: int(t.Price),
		})
	}

	planSeats := gen.PlanSeats{
		Min:      1,
		Max:      100,
		Included: 1,
	}
	if p.Seats != nil {
		planSeats.Min = p.Seats.Min
		if p.Seats.Max != nil {
			planSeats.Max = *p.Seats.Max
		}
		planSeats.Included = p.Seats.Min
	}

	monthly := int(p.Pricing.Monthly)
	annual := int(p.Pricing.Annual)

	effectiveFrom := time.Now().UTC()
	if p.EffectiveFrom != nil {
		effectiveFrom = *p.EffectiveFrom
	}

	return gen.BillingPlan{
		Code:          p.Code,
		Version:       p.Version,
		Kind:          gen.BillingPlanKind(p.Kind),
		Name:          p.Name["es"],
		EffectiveFrom: effectiveFrom,
		Features:      p.Features,
		Pricing: gen.PlanPricing{
			Model:    gen.PlanPricingModel(p.Pricing.Model),
			Currency: p.Pricing.Currency,
			Monthly:  &monthly,
			Annual:   &annual,
			Tiers:    &tiers,
		},
		Quotas:    quotas,
		Seats:     planSeats,
		Status:    gen.BillingPlanStatus(p.Status),
		TrialDays: p.TrialDays,
	}
}

func toGenAccount(a *infra.AccountView) gen.BillingAccount {
	if a == nil {
		return gen.BillingAccount{}
	}

	var pm *gen.PaymentMethodSummary
	if a.PaymentMethod != nil {
		var expStr *string
		if a.PaymentMethod.ExpiresAt != nil {
			s := a.PaymentMethod.ExpiresAt.Format("01/06")
			expStr = &s
		}
		pm = &gen.PaymentMethodSummary{
			Kind:      a.PaymentMethod.Kind,
			Last4:     a.PaymentMethod.Last4,
			ExpiresAt: expStr,
		}
	}

	return gen.BillingAccount{
		Id:            a.ID,
		Country:       a.Country,
		Currency:      a.Currency,
		CreatedAt:     a.CreatedAt,
		PaymentMethod: pm,
		Organizations: a.Organizations,
		LegalEntity: gen.LegalEntity{
			Kind:      gen.LegalEntityKind(a.LegalEntity.Kind),
			LegalName: a.LegalEntity.LegalName,
			Address:   a.LegalEntity.Address,
			Email:     openapi_types.Email(a.LegalEntity.Email),
			Phone:     a.LegalEntity.Phone,
			TaxRegime: a.LegalEntity.TaxRegime,
			TaxId: gen.TaxID{
				Country: a.LegalEntity.TaxID.Country,
				Value:   a.LegalEntity.TaxID.Value,
				Kind:    a.LegalEntity.TaxID.Kind,
			},
		},
	}
}

func toGenSubscription(s *infra.SubscriptionView) gen.Subscription {
	if s == nil {
		return gen.Subscription{}
	}

	var seats *gen.SubscriptionSeats
	if s.SeatsPurchased != nil {
		seats = &gen.SubscriptionSeats{
			Purchased: *s.SeatsPurchased,
			Used:      *s.SeatsPurchased,
		}
	}

	var pending *gen.PendingChange
	if s.PendingChange != nil {
		var tier *int
		if s.PendingChange.Tier != nil {
			if parsed, err := strconv.Atoi(*s.PendingChange.Tier); err == nil {
				tier = &parsed
			}
		}
		cycleStr := string(s.PendingChange.Cycle)
		pending = &gen.PendingChange{
			EffectiveAt: s.PendingChange.ScheduledAt,
			PlanCode:    &s.PendingChange.PlanCode,
			Seats:       s.PendingChange.Seats,
			Tier:        tier,
			Cycle:       &cycleStr,
		}
	}

	var tierVal *int
	if s.Tier != nil {
		if parsed, err := strconv.Atoi(*s.Tier); err == nil {
			tierVal = &parsed
		}
	}

	var charges []gen.SubscriptionCharge
	if len(s.Charges) > 0 {
		charges = make([]gen.SubscriptionCharge, 0, len(s.Charges))
		for _, c := range s.Charges {
			charges = append(charges, gen.SubscriptionCharge{
				Id:        c.ID,
				Number:    c.ID.String()[:8],
				Amount:    int(c.Total),
				Currency:  c.Currency,
				Status:    string(c.Status),
				DueAt:     &c.ScheduledAt,
				PaidAt:    c.PaidAt,
				CreatedAt: c.ScheduledAt,
			})
		}
	}

	return gen.Subscription{
		Id:                s.ID,
		BillingAccountId:  s.AccountID,
		Kind:              gen.SubscriptionKind(s.Kind),
		Status:            gen.SubscriptionStatus(s.Status),
		Cycle:             gen.SubscriptionCycle(s.Cycle),
		TrialEndsAt:       s.TrialEndsAt,
		CancelAtPeriodEnd: s.CancelAtPeriodEnd,
		CreatedAt:         s.CreatedAt,
		Tier:              tierVal,
		Seats:             seats,
		PendingChange:     pending,
		Charges:           &charges,
		Plan: gen.PlanRef{
			Code:    s.PlanCode,
			Version: s.PlanVersion,
			Name:    s.PlanCode,
		},
		CurrentPeriod: gen.SubscriptionPeriod{
			Start: s.PeriodStart,
			End:   s.PeriodEnd,
		},
	}
}

func toGenEntitlements(e *kernel.EntitlementSet) gen.EntitlementSet {
	if e == nil {
		return gen.EntitlementSet{}
	}

	quotas := make(map[string]gen.QuotaItem, len(e.Quotas))
	for k, q := range e.Quotas {
		var pEnd time.Time
		if q.PeriodEnd != nil {
			pEnd = *q.PeriodEnd
		}
		quotas[string(k)] = gen.QuotaItem{
			Limit:     int(q.Limit),
			Used:      int(q.Used),
			SoftLimit: int(q.SoftLimit),
			PeriodEnd: pEnd,
		}
	}

	return gen.EntitlementSet{
		OrganizationId: e.OrgID,
		PlanCode:       e.PlanCode,
		Status:         string(e.Status),
		Features:       e.Features,
		Seats: struct {
			Limit int `json:"limit"`
			Used  int `json:"used"`
		}{
			Limit: e.Seats.Limit,
			Used:  e.Seats.Used,
		},
		Quotas: quotas,
	}
}

// -----------------------------------------------------------------------------
// Planes (PP-F5-API-001)
// -----------------------------------------------------------------------------

// ListPlans devuelve la lista pública de planes vigentes.
func (h *BillingHandlers) ListPlans(ctx context.Context, req gen.ListPlansRequestObject) (gen.ListPlansResponseObject, error) {
	if h.queries == nil {
		return gen.ListPlans200JSONResponse{Data: []gen.BillingPlan{}}, nil
	}

	var kindPtr *string
	if req.Params.Kind != nil {
		k := string(*req.Params.Kind)
		kindPtr = &k
	}

	plans, err := h.queries.GetPlans(ctx, kindPtr, req.Params.Country)
	if err != nil {
		return nil, err
	}

	data := make([]gen.BillingPlan, 0, len(plans))
	for _, p := range plans {
		data = append(data, toGenPlan(p))
	}

	return gen.ListPlans200JSONResponse{Data: data}, nil
}

// GetPlan devuelve el plan vigente según código.
func (h *BillingHandlers) GetPlan(ctx context.Context, req gen.GetPlanRequestObject) (gen.GetPlanResponseObject, error) {
	if h.queries == nil {
		return gen.GetPlan200JSONResponse{}, nil
	}

	plan, err := h.queries.GetPlan(ctx, req.Code)
	if err != nil {
		return nil, &kernel.NotFoundError{Resource: "Plan " + req.Code}
	}
	return gen.GetPlan200JSONResponse(toGenPlan(*plan)), nil
}

// CreatePlan crea un nuevo plan (super_admin).
func (h *BillingHandlers) CreatePlan(ctx context.Context, req gen.CreatePlanRequestObject) (gen.CreatePlanResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AdminPlansWrite, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.queries == nil {
		return gen.CreatePlan201JSONResponse{}, nil
	}

	body := req.Body
	planDef := domain.PlanDefinition{
		Code:          body.Code,
		Version:       1,
		Kind:          string(body.Kind),
		Name:          map[string]string{"es": body.Name},
		TrialDays:     body.TrialDays,
		Features:      body.Features,
		Status:        "draft",
		EffectiveFrom: &body.EffectiveFrom,
		PublishedAt:   &body.EffectiveFrom,
	}

	planDef.Pricing = domain.PlanPricing{
		Model:    string(body.Pricing.Model),
		Currency: body.Pricing.Currency,
	}
	if body.Pricing.Monthly != nil {
		planDef.Pricing.Monthly = float64(*body.Pricing.Monthly)
	}
	if body.Pricing.Annual != nil {
		planDef.Pricing.Annual = float64(*body.Pricing.Annual)
	}
	if body.Pricing.Tiers != nil {
		for _, t := range *body.Pricing.Tiers {
			planDef.Pricing.Tiers = append(planDef.Pricing.Tiers, domain.VolumeTier{
				UpTo:  t.UpTo,
				Price: float64(t.MonthlyPrice),
			})
		}
	}

	planDef.Seats = &domain.PlanSeatsRange{
		Min: body.Seats.Min,
		Max: &body.Seats.Max,
	}

	planDef.Quotas = make(map[string]int64, len(body.Quotas))
	for k, v := range body.Quotas {
		planDef.Quotas[k] = int64(v)
	}

	if err := h.queries.UpsertPlan(ctx, planDef); err != nil {
		return nil, err
	}

	return gen.CreatePlan201JSONResponse(toGenPlan(planDef)), nil
}

// UpdatePlan actualiza un plan existente (super_admin).
func (h *BillingHandlers) UpdatePlan(ctx context.Context, req gen.UpdatePlanRequestObject) (gen.UpdatePlanResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AdminPlansWrite, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.queries == nil {
		return gen.UpdatePlan200JSONResponse{}, nil
	}

	existing, err := h.queries.GetPlan(ctx, req.Code)
	if err != nil {
		return nil, &kernel.NotFoundError{Resource: "Plan " + req.Code}
	}

	body := req.Body
	planDef := domain.PlanDefinition{
		Code:          req.Code,
		Version:       existing.Version + 1,
		Kind:          string(body.Kind),
		Name:          map[string]string{"es": body.Name},
		TrialDays:     body.TrialDays,
		Features:      body.Features,
		Status:        "published",
		EffectiveFrom: &body.EffectiveFrom,
		PublishedAt:   &body.EffectiveFrom,
	}

	planDef.Pricing = domain.PlanPricing{
		Model:    string(body.Pricing.Model),
		Currency: body.Pricing.Currency,
	}
	if body.Pricing.Monthly != nil {
		planDef.Pricing.Monthly = float64(*body.Pricing.Monthly)
	}
	if body.Pricing.Annual != nil {
		planDef.Pricing.Annual = float64(*body.Pricing.Annual)
	}

	planDef.Seats = &domain.PlanSeatsRange{
		Min: body.Seats.Min,
		Max: &body.Seats.Max,
	}

	planDef.Quotas = make(map[string]int64, len(body.Quotas))
	for k, v := range body.Quotas {
		planDef.Quotas[k] = int64(v)
	}

	if err := h.queries.UpsertPlan(ctx, planDef); err != nil {
		return nil, err
	}

	return gen.UpdatePlan200JSONResponse(toGenPlan(planDef)), nil
}

// -----------------------------------------------------------------------------
// Cuenta de Facturación (PP-F5-API-002)
// -----------------------------------------------------------------------------

// GetOrgBilling obtiene el resumen de facturación de la organización.
func (h *BillingHandlers) GetOrgBilling(ctx context.Context, req gen.GetOrgBillingRequestObject) (gen.GetOrgBillingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingRead, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.GetOrgBilling200JSONResponse{}, nil
	}

	var summary gen.OrgBillingSummary

	account, _ := h.queries.GetAccountByOrg(ctx, req.OrgId)
	if account != nil {
		acc := toGenAccount(account)
		summary.Account = &acc

		sub, _ := h.queries.GetSubscriptionByAccount(ctx, account.ID, domain.KindClinic)
		if sub == nil {
			sub, _ = h.queries.GetSubscriptionByAccount(ctx, account.ID, domain.KindPharmacy)
		}
		if sub != nil {
			s := toGenSubscription(sub)
			summary.Subscription = &s
		}
	}

	ent, _ := h.queries.GetEntitlements(ctx, req.OrgId)
	if ent != nil {
		e := toGenEntitlements(ent)
		summary.Entitlements = &e
	}

	return gen.GetOrgBilling200JSONResponse(summary), nil
}

// OpenBillingAccount abre una nueva cuenta de facturación.
func (h *BillingHandlers) OpenBillingAccount(ctx context.Context, req gen.OpenBillingAccountRequestObject) (gen.OpenBillingAccountResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	targetOrg := uuid.Nil
	if req.Body != nil {
		targetOrg = req.Body.OrganizationId
	}

	if d := authz.Can(p, authz.BillingManage, authz.OrgResource{ID: targetOrg}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.OpenBillingAccount201JSONResponse{}, nil
	}

	body := req.Body
	le := domain.LegalEntity{
		Kind:      string(body.LegalEntity.Kind),
		LegalName: body.LegalEntity.LegalName,
		Address:   body.LegalEntity.Address,
		Email:     string(body.LegalEntity.Email),
		Phone:     body.LegalEntity.Phone,
		TaxRegime: body.LegalEntity.TaxRegime,
		TaxID: domain.TaxID{
			Country: body.LegalEntity.TaxId.Country,
			Value:   body.LegalEntity.TaxId.Value,
			Kind:    body.LegalEntity.TaxId.Kind,
		},
	}

	cmd := app.OpenAccountCmd{
		AccountID:   uuid.New(),
		OwnerUserID: p.UserID,
		LegalEntity: le,
		Country:     body.LegalEntity.TaxId.Country,
		OrgID:       &body.OrganizationId,
	}

	acc, err := h.svc.OpenAccount(ctx, cmd)
	if err != nil {
		return nil, mapDomainError(err)
	}

	view, err := h.queries.GetAccount(ctx, acc.ID())
	if err != nil || view == nil {
		return nil, &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	}

	return gen.OpenBillingAccount201JSONResponse(toGenAccount(view)), nil
}

// UpdateBillingAccount actualiza los datos fiscales de la cuenta.
func (h *BillingHandlers) UpdateBillingAccount(ctx context.Context, req gen.UpdateBillingAccountRequestObject) (gen.UpdateBillingAccountResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.accountRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.UpdateBillingAccount200JSONResponse{}, nil
	}

	acc, err := h.queries.GetAccount(ctx, req.Id)
	if err != nil || acc == nil {
		return nil, &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	}

	return gen.UpdateBillingAccount200JSONResponse(toGenAccount(acc)), nil
}

// CloseBillingAccount cierra la cuenta de facturación.
func (h *BillingHandlers) CloseBillingAccount(ctx context.Context, req gen.CloseBillingAccountRequestObject) (gen.CloseBillingAccountResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.accountRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.svc == nil {
		return gen.CloseBillingAccount200JSONResponse{}, nil
	}

	if err := h.svc.CloseAccount(ctx, req.Id); err != nil {
		return nil, mapDomainError(err)
	}

	acc, err := h.queries.GetAccount(ctx, req.Id)
	if err != nil || acc == nil {
		return nil, &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	}

	return gen.CloseBillingAccount200JSONResponse(toGenAccount(acc)), nil
}

// -----------------------------------------------------------------------------
// Suscripciones (PP-F5-API-003)
// -----------------------------------------------------------------------------

// ListAccountSubscriptions lista las suscripciones de la cuenta.
func (h *BillingHandlers) ListAccountSubscriptions(ctx context.Context, req gen.ListAccountSubscriptionsRequestObject) (gen.ListAccountSubscriptionsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingRead, h.accountRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.ListAccountSubscriptions200JSONResponse{Data: []gen.Subscription{}}, nil
	}

	subs, err := h.queries.ListAccountSubscriptions(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	data := make([]gen.Subscription, 0, len(subs))
	for _, s := range subs {
		data = append(data, toGenSubscription(&s))
	}

	return gen.ListAccountSubscriptions200JSONResponse{Data: data}, nil
}

// PreviewSubscription previsualiza cálculo o prorrateo sin escribir.
func (h *BillingHandlers) PreviewSubscription(ctx context.Context, req gen.PreviewSubscriptionRequestObject) (gen.PreviewSubscriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.accountRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.PreviewSubscription200JSONResponse{}, nil
	}

	body := req.Body
	var tierStr *string
	if body.Tier != nil {
		t := strconv.Itoa(*body.Tier)
		tierStr = &t
	}

	preview, err := h.svc.PreviewSubscription(ctx, app.PreviewSubscriptionCmd{
		AccountID: req.Id,
		Kind:      domain.SubscriptionKind(body.Kind),
		PlanCode:  body.PlanCode,
		Seats:     body.Seats,
		Tier:      tierStr,
		Cycle:     domain.BillingCycle(body.Cycle),
		Coupon:    body.Coupon,
	})
	if err != nil {
		return nil, mapDomainError(err)
	}

	lines := make([]gen.PreviewLine, 0, len(preview.Lines))
	for _, l := range preview.Lines {
		lines = append(lines, gen.PreviewLine{
			Description: l.Description,
			Amount:      int(l.Amount),
		})
	}

	var proration *int
	if preview.Proration != nil {
		p := int(*preview.Proration)
		proration = &p
	}

	return gen.PreviewSubscription200JSONResponse{
		Currency:        preview.Currency,
		Subtotal:        int(preview.Subtotal),
		Tax:             int(preview.Tax),
		Total:           int(preview.Total),
		ProrationCredit: proration,
		Lines:           lines,
		NextChargeAt:    preview.NextChargeAt,
	}, nil
}

// CreateSubscription contrata una suscripción.
func (h *BillingHandlers) CreateSubscription(ctx context.Context, req gen.CreateSubscriptionRequestObject) (gen.CreateSubscriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.accountRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.CreateSubscription201JSONResponse{}, nil
	}

	body := req.Body
	var tierStr *string
	if body.Tier != nil {
		t := strconv.Itoa(*body.Tier)
		tierStr = &t
	}

	sub, err := h.svc.Subscribe(ctx, app.SubscribeCmd{
		SubscriptionID: uuid.New(),
		AccountID:      req.Id,
		Kind:           domain.SubscriptionKind(body.Kind),
		PlanCode:       body.PlanCode,
		Seats:          body.Seats,
		Tier:           tierStr,
		Cycle:          domain.BillingCycle(body.Cycle),
		CouponCode:     body.Coupon,
	})
	if err != nil {
		return nil, mapDomainError(err)
	}

	view, err := h.queries.GetSubscription(ctx, sub.ID())
	if err != nil || view == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.CreateSubscription201JSONResponse(toGenSubscription(view)), nil
}

// GetSubscription obtiene una suscripción por ID.
func (h *BillingHandlers) GetSubscription(ctx context.Context, req gen.GetSubscriptionRequestObject) (gen.GetSubscriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingRead, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.GetSubscription200JSONResponse{}, nil
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.GetSubscription200JSONResponse(toGenSubscription(sub)), nil
}

// ChangeSubscriptionPlan cambia el plan o ciclo de la suscripción.
func (h *BillingHandlers) ChangeSubscriptionPlan(ctx context.Context, req gen.ChangeSubscriptionPlanRequestObject) (gen.ChangeSubscriptionPlanResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.ChangeSubscriptionPlan200JSONResponse{}, nil
	}

	body := req.Body
	var tierStr *string
	if body.Tier != nil {
		t := strconv.Itoa(*body.Tier)
		tierStr = &t
	}

	cycle := domain.BillingCycle(domain.CycleMonthly)
	if body.Cycle != nil {
		cycle = domain.BillingCycle(*body.Cycle)
	}

	planCode := ""
	if body.PlanCode != nil {
		planCode = *body.PlanCode
	}

	if err := h.svc.ChangePlan(ctx, req.Id, planCode, body.Seats, tierStr, cycle); err != nil {
		return nil, mapDomainError(err)
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.ChangeSubscriptionPlan200JSONResponse(toGenSubscription(sub)), nil
}

// AdjustSubscriptionSeats ajusta los asientos de una suscripción.
func (h *BillingHandlers) AdjustSubscriptionSeats(ctx context.Context, req gen.AdjustSubscriptionSeatsRequestObject) (gen.AdjustSubscriptionSeatsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.AdjustSubscriptionSeats200JSONResponse{}, nil
	}

	if err := h.svc.AdjustSeats(ctx, req.Id, req.Body.Delta); err != nil {
		return nil, mapDomainError(err)
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.AdjustSubscriptionSeats200JSONResponse(toGenSubscription(sub)), nil
}

// CancelSubscription cancela la suscripción.
func (h *BillingHandlers) CancelSubscription(ctx context.Context, req gen.CancelSubscriptionRequestObject) (gen.CancelSubscriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || h.svc == nil {
		return gen.CancelSubscription200JSONResponse{}, nil
	}

	reason := ""
	if req.Body.Reason != nil {
		reason = *req.Body.Reason
	}

	if err := h.svc.CancelSubscription(ctx, req.Id, req.Body.AtPeriodEnd, reason); err != nil {
		return nil, mapDomainError(err)
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.CancelSubscription200JSONResponse(toGenSubscription(sub)), nil
}

// ResumeSubscription reanuda una suscripción cancelada a fin de periodo.
func (h *BillingHandlers) ResumeSubscription(ctx context.Context, req gen.ResumeSubscriptionRequestObject) (gen.ResumeSubscriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.svc == nil {
		return gen.ResumeSubscription200JSONResponse{}, nil
	}

	if err := h.svc.ResumeSubscription(ctx, req.Id); err != nil {
		return nil, mapDomainError(err)
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.ResumeSubscription200JSONResponse(toGenSubscription(sub)), nil
}

// RetrySubscriptionCharge reintenta un cobro fallido.
func (h *BillingHandlers) RetrySubscriptionCharge(ctx context.Context, req gen.RetrySubscriptionChargeRequestObject) (gen.RetrySubscriptionChargeResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingManage, h.subscriptionRes(ctx, req.Id)); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.svc == nil {
		return gen.RetrySubscriptionCharge200JSONResponse{}, nil
	}

	if err := h.svc.RetryCharge(ctx, req.Id); err != nil {
		return nil, mapDomainError(err)
	}

	sub, err := h.queries.GetSubscription(ctx, req.Id)
	if err != nil || sub == nil {
		return nil, &kernel.NotFoundError{Resource: "Suscripción"}
	}

	return gen.RetrySubscriptionCharge200JSONResponse(toGenSubscription(sub)), nil
}

// -----------------------------------------------------------------------------
// Entitlements y Uso (PP-F5-API-006)
// -----------------------------------------------------------------------------

// GetOrgEntitlements consulta los entitlements de la organización.
func (h *BillingHandlers) GetOrgEntitlements(ctx context.Context, req gen.GetOrgEntitlementsRequestObject) (gen.GetOrgEntitlementsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingRead, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.GetOrgEntitlements200JSONResponse{}, nil
	}

	ent, err := h.queries.GetEntitlements(ctx, req.OrgId)
	if err != nil || ent == nil {
		return nil, &kernel.NotFoundError{Resource: "Entitlements de la organización"}
	}

	return gen.GetOrgEntitlements200JSONResponse(toGenEntitlements(ent)), nil
}

// GetOrgUsage consulta los contadores de uso del periodo.
func (h *BillingHandlers) GetOrgUsage(ctx context.Context, req gen.GetOrgUsageRequestObject) (gen.GetOrgUsageResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.BillingRead, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if h.queries == nil {
		return gen.GetOrgUsage200JSONResponse{}, nil
	}

	usage, err := h.queries.GetUsage(ctx, req.OrgId, req.Params.From, req.Params.To)
	if err != nil {
		return nil, err
	}

	start := time.Now().UTC()
	end := start.AddDate(0, 1, 0)
	if len(usage) > 0 {
		start = usage[0].PeriodStart
		end = usage[0].PeriodEnd
	}

	counters := make(map[string]gen.UsageCounterItem, len(usage))
	for _, u := range usage {
		counters[u.QuotaKey] = gen.UsageCounterItem{
			CounterKey:  u.QuotaKey,
			Used:        int(u.Used),
			PeriodStart: u.PeriodStart,
			PeriodEnd:   u.PeriodEnd,
		}
	}

	return gen.GetOrgUsage200JSONResponse{
		OrganizationId: req.OrgId,
		PeriodStart:    start,
		PeriodEnd:      end,
		Counters:       counters,
	}, nil
}
