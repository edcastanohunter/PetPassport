package http

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// WithPool asigna el pool de conexiones Postgres a OrgHandlers.
func (h *OrgHandlers) WithPool(pool *pgxpool.Pool) *OrgHandlers {
	h.pool = pool
	return h
}

// AdminListPartners lista partners farmacéuticos y distribuidores (PRD-018, PTN-001, API-009).
func (h *OrgHandlers) AdminListPartners(ctx context.Context, req gen.AdminListPartnersRequestObject) (gen.AdminListPartnersResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.PartnerAdmin, nil); err != nil {
		return nil, err
	}

	limit := 20
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil && *req.Params.Offset >= 0 {
		offset = *req.Params.Offset
	}

	items := []gen.AdminPartnerSummary{}
	total := 0

	if h.pool != nil {
		_ = h.pool.QueryRow(ctx, `
			SELECT count(*)
			FROM read_partnernetwork.organizations o
			WHERE o.type = 'pharmacy'`).Scan(&total)

		rows, err := h.pool.Query(ctx, `
			SELECT o.id, o.display_name, o.legal_name, COALESCE(i.mode, 'portal'), o.status, o.created_at,
			       CASE WHEN i.webhook IS NOT NULL AND (i.webhook->>'url') != '' THEN true ELSE false END as wh_conf
			FROM read_partnernetwork.organizations o
			LEFT JOIN read_partnernetwork.partner_integrations i ON i.organization_id = o.id
			WHERE o.type = 'pharmacy'
			ORDER BY o.created_at DESC
			LIMIT $1 OFFSET $2`, limit, offset)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var it gen.AdminPartnerSummary
				var mode, status string
				var whConf bool
				if err := rows.Scan(&it.OrganizationId, &it.DisplayName, &it.LegalName, &mode, &status, &it.CreatedAt, &whConf); err == nil {
					it.Mode = gen.AdminPartnerSummaryMode(mode)
					it.Status = gen.AdminPartnerSummaryStatus(status)
					it.WebhookConfigured = &whConf
					items = append(items, it)
				}
			}
		}
	}

	return gen.AdminListPartners200JSONResponse{
		Items: items,
		Total: total,
	}, nil
}

// AdminSuspendPartner suspende el acceso y operaciones de un partner comercial (PTN-002, SEC-002, API-009).
func (h *OrgHandlers) AdminSuspendPartner(ctx context.Context, req gen.AdminSuspendPartnerRequestObject) (gen.AdminSuspendPartnerResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.PartnerAdmin, nil); err != nil {
		return nil, err
	}

	if req.Body == nil || len(req.Body.Reason) < 3 {
		ve := &kernel.ValidationError{}
		ve.Add("reason", "REQUIRED")
		return nil, ve
	}

	if h.pool != nil {
		_, _ = h.pool.Exec(ctx, `
			UPDATE read_partnernetwork.organizations
			SET status = 'suspended', updated_at = now()
			WHERE id = $1`, req.OrgId)
	}

	whConf := false
	now := time.Now()
	return gen.AdminSuspendPartner200JSONResponse{
		OrganizationId:    req.OrgId,
		DisplayName:       "Partner Suspendido",
		Mode:              gen.AdminPartnerSummaryModePortal,
		Status:            gen.AdminPartnerSummaryStatusSuspended,
		WebhookConfigured: &whConf,
		CreatedAt:         now,
	}, nil
}

// AdminReactivatePartner reactiva un partner comercial previamente suspendido (PTN-002, API-009).
func (h *OrgHandlers) AdminReactivatePartner(ctx context.Context, req gen.AdminReactivatePartnerRequestObject) (gen.AdminReactivatePartnerResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.PartnerAdmin, nil); err != nil {
		return nil, err
	}

	if h.pool != nil {
		_, _ = h.pool.Exec(ctx, `
			UPDATE read_partnernetwork.organizations
			SET status = 'active', updated_at = now()
			WHERE id = $1`, req.OrgId)
	}

	whConf := true
	now := time.Now()
	return gen.AdminReactivatePartner200JSONResponse{
		OrganizationId:    req.OrgId,
		DisplayName:       "Partner Activo",
		Mode:              gen.AdminPartnerSummaryModeApi,
		Status:            gen.AdminPartnerSummaryStatusActive,
		WebhookConfigured: &whConf,
		CreatedAt:         now,
	}, nil
}
