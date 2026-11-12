package infra

import (
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// RegisterEvents registra todos los eventos de Billing v1 en el Registry (PP-F5-DOM-006).
func RegisterEvents(reg *eventstore.Registry) {
	// Account events
	reg.Register(func() eventstore.Event { return domain.BillingAccountOpened{} })
	reg.Register(func() eventstore.Event { return domain.LegalEntityUpdated{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationAttached{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationDetached{} })
	reg.Register(func() eventstore.Event { return domain.PaymentMethodSet{} })
	reg.Register(func() eventstore.Event { return domain.BillingAccountClosed{} })

	// Subscription events
	reg.Register(func() eventstore.Event { return domain.TrialStarted{} })
	reg.Register(func() eventstore.Event { return domain.Subscribed{} })
	reg.Register(func() eventstore.Event { return domain.PlanChangeApplied{} })
	reg.Register(func() eventstore.Event { return domain.PlanChangeScheduled{} })
	reg.Register(func() eventstore.Event { return domain.SeatsAdjusted{} })
	reg.Register(func() eventstore.Event { return domain.PeriodRenewed{} })
	reg.Register(func() eventstore.Event { return domain.ChargeScheduled{} })
	reg.Register(func() eventstore.Event { return domain.ChargePaid{} })
	reg.Register(func() eventstore.Event { return domain.ChargeFailed{} })
	reg.Register(func() eventstore.Event { return domain.ChargeRefunded{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionGraceStarted{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionRestricted{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionReactivated{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionCancellationScheduled{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionCancelled{} })
	reg.Register(func() eventstore.Event { return domain.SubscriptionResumed{} })
	reg.Register(func() eventstore.Event { return domain.TrialExpired{} })
	reg.Register(func() eventstore.Event { return domain.AdminAdjustmentApplied{} })
	reg.Register(func() eventstore.Event { return domain.CouponAppliedEvent{} })
}
