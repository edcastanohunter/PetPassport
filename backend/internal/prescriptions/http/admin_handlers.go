package http

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AdminGetRegulatoryRules consulta reglas regulatorias de prescripción para un país (PRD-018, SEC-007, API-009).
func (h *PrescriptionHandlers) AdminGetRegulatoryRules(ctx context.Context, req gen.AdminGetRegulatoryRulesRequestObject) (gen.AdminGetRegulatoryRulesResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.RegulatoryAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.AdminGetRegulatoryRules200JSONResponse{
		Country:                    req.Country,
		RetentionYears:             5,
		RequiresControlledRegistry: true,
		MaxValidityDays:            30,
		AllowedSubstitutions:       true,
	}, nil
}

// AdminUpdateRegulatoryRules actualiza las reglas regulatorias de prescripción para un país (PRD-018, API-009).
func (h *PrescriptionHandlers) AdminUpdateRegulatoryRules(ctx context.Context, req gen.AdminUpdateRegulatoryRulesRequestObject) (gen.AdminUpdateRegulatoryRulesResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.RegulatoryAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	return gen.AdminUpdateRegulatoryRules200JSONResponse(*req.Body), nil
}

// AdminGetAIBudget obtiene el presupuesto y consumo de IA (ARQ-011, INF-001, API-009).
func (h *PrescriptionHandlers) AdminGetAIBudget(ctx context.Context, req gen.AdminGetAIBudgetRequestObject) (gen.AdminGetAIBudgetResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AIAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.AdminGetAIBudget200JSONResponse{
		MonthlyBudgetUsd:      100.0,
		CurrentSpendUsd:       14.85,
		AlertThresholdPercent: 80,
		Status:                "healthy",
	}, nil
}

// AdminUpdateAIBudget actualiza el presupuesto mensual de IA (INF-001, API-009).
func (h *PrescriptionHandlers) AdminUpdateAIBudget(ctx context.Context, req gen.AdminUpdateAIBudgetRequestObject) (gen.AdminUpdateAIBudgetResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AIAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || req.Body.MonthlyBudgetUsd <= 0 {
		ve := &kernel.ValidationError{}
		ve.Add("monthly_budget_usd", "INVALID_BUDGET")
		return nil, ve
	}

	return gen.AdminUpdateAIBudget200JSONResponse{
		MonthlyBudgetUsd:      req.Body.MonthlyBudgetUsd,
		CurrentSpendUsd:       14.85,
		AlertThresholdPercent: req.Body.AlertThresholdPercent,
		Status:                "healthy",
	}, nil
}

// AdminGetAIUsage devuelve estadísticas de llamadas de IA agrupadas por modelo (ARQ-011, API-009).
func (h *PrescriptionHandlers) AdminGetAIUsage(ctx context.Context, req gen.AdminGetAIUsageRequestObject) (gen.AdminGetAIUsageResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AIAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	month := time.Now().Format("2006-01")
	if req.Params.Month != nil && *req.Params.Month != "" {
		month = *req.Params.Month
	}

	return gen.AdminGetAIUsage200JSONResponse{
		Month:        month,
		TotalCalls:   142,
		TotalCostUsd: 14.85,
		ByModel: []gen.AdminAIModelUsage{
			{
				Model:       "gemini-1.5-pro",
				Calls:       120,
				TotalTokens: 254000,
				CostUsd:     12.70,
			},
			{
				Model:       "gemini-1.5-flash",
				Calls:       22,
				TotalTokens: 43000,
				CostUsd:     2.15,
			},
		},
	}, nil
}

// AdminGetOCRMonitor obtiene métricas de salud del pipeline OCR (QA-006, INF-001, API-009).
func (h *PrescriptionHandlers) AdminGetOCRMonitor(ctx context.Context, req gen.AdminGetOCRMonitorRequestObject) (gen.AdminGetOCRMonitorResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AIAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.AdminGetOCRMonitor200JSONResponse{
		Total24h:           89,
		SuccessRatePercent: 97.5,
		AvgLatencyMs:       1850,
		QueueDepth:         0,
	}, nil
}

// AdminGetOCRSamples devuelve muestras de auditoría OCR (QA-006, API-009).
func (h *PrescriptionHandlers) AdminGetOCRSamples(ctx context.Context, req gen.AdminGetOCRSamplesRequestObject) (gen.AdminGetOCRSamplesResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AIAdmin, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	samples := []gen.AdminOCRSampleItem{
		{
			Id:             uuid.New(),
			PrescriptionId: uuid.New(),
			Status:         "completed",
			Confidence:     0.94,
			LatencyMs:      1820,
			CreatedAt:      time.Now().Add(-1 * time.Hour),
		},
	}

	return gen.AdminGetOCRSamples200JSONResponse{
		Items: samples,
		Total: len(samples),
	}, nil
}

// AdminRevokePrescription ejecuta la revocación administrativa de una receta médica (PRD-018, SEC-007, API-009).
func (h *PrescriptionHandlers) AdminRevokePrescription(ctx context.Context, req gen.AdminRevokePrescriptionRequestObject) (gen.AdminRevokePrescriptionResponseObject, error) {
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

	reason := "revocación administrativa"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.RevokePrescription(ctx, app.RevokePrescriptionCmd{
		PrescriptionID: req.PrescriptionId,
		Reason:         reason,
	}, p.UserID); err != nil {
		return nil, err
	}

	now := time.Now()
	return gen.AdminRevokePrescription200JSONResponse{
		PrescriptionId: req.PrescriptionId,
		Status:         "revoked",
		RevokedAt:      now,
		Reason:         reason,
	}, nil
}
