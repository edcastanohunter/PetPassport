package main

import (
	"log/slog"

	billinghttp "github.com/edcastanohunter/petpassport/backend/internal/billing/http"
	billinginfra "github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
)

func newSuiteBillingHandlers(logger *slog.Logger) *billinghttp.BillingHandlers {
	queries := billinginfra.NewQueries(nil)
	return billinghttp.NewBillingHandlers(nil, queries, logger)
}

// billingMatrix: columnas = tutor vetL admL recL vetU vetS phAd str sup
var billingMatrix = map[string][9]int{
	// super_admin only
	"createPlan": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"updatePlan": {403, 403, 403, 403, 403, 403, 403, 403, ok},

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
}

func init() {
	for k, v := range billingMatrix {
		matrix[k] = v
	}
}
