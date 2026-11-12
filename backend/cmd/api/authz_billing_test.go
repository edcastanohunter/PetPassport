package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	billinghttp "github.com/edcastanohunter/petpassport/backend/internal/billing/http"
	billinginfra "github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
)

// suiteBillingScope: toda cuenta agrupa la clínica y la farmacia del fixture, de
// modo que la matriz ejerce la regla real (admin de una organización de la
// cuenta) y no un permiso genérico por rol.
type suiteBillingScope struct{}

func (suiteBillingScope) AccountOrgs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{orgActive, orgPharma}, nil
}

func (suiteBillingScope) SubscriptionAccount(context.Context, uuid.UUID) (uuid.UUID, error) {
	return rxID, nil
}

type suitePresigner struct{}

func (suitePresigner) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.invalid/" + key, nil
}

func newSuiteBillingHandlers(logger *slog.Logger) *billinghttp.BillingHandlers {
	queries := billinginfra.NewQueries(nil)
	return billinghttp.NewBillingHandlers(nil, queries, logger).
		WithScope(suiteBillingScope{}).
		WithPayments(billinghttp.PaymentDeps{Objects: suitePresigner{}})
}

// billingMatrix: columnas = tutor vetL admL recL vetU vetS phAd str sup
var billingMatrix = map[string][9]int{
	// super_admin only
	"createPlan": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"updatePlan": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	// M5.2 — versionado de planes (API-001, FE-005)
	"adminListPlanVersions": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"publishPlan":           {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"retirePlan":            {403, 403, 403, 403, 403, 403, 403, 403, ok},

	// billing:read
	"getOrgBilling":            {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"listAccountSubscriptions": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getSubscription":          {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getOrgEntitlements":       {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getOrgUsage":              {403, 403, ok, 403, 403, 403, ok, 403, ok},

	// billing:manage
	"openBillingAccount":      {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"updateBillingAccount":    {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"closeBillingAccount":     {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"previewSubscription":     {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"createSubscription":      {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"changeSubscriptionPlan":  {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"adjustSubscriptionSeats": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"cancelSubscription":      {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"resumeSubscription":      {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"retrySubscriptionCharge": {403, 403, ok, 403, 403, 403, ok, 403, ok},

	// M5.2 — billing:manage / billing:read sobre la cuenta (API-004, API-005)
	"setupPaymentMethod":  {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"attachPaymentMethod": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"removePaymentMethod": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"listAccountInvoices": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"listAccountPayments": {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getInvoicePdf":       {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getInvoiceXml":       {403, 403, ok, 403, 403, 403, ok, 403, ok},

	// M5.2 — administración de facturación, solo super_admin (API-007)
	"adminListBillingAccounts":       {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminGetBillingAccount":         {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminAdjustSubscription":        {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminCancelSubscription":        {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminRecordManualPayment":       {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminRefundCharge":              {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminListBillingDiscrepancies":  {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminResolveBillingDiscrepancy": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminListBillingInvoices":       {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminRetryInvoice":              {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminListCoupons":               {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"adminCreateCoupon":              {403, 403, 403, 403, 403, 403, 403, 403, ok},
}

func init() {
	for k, v := range billingMatrix {
		matrix[k] = v
	}
}
