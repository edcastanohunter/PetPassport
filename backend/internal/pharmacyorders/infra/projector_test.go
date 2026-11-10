package infra

import (
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

func TestPharmacyOrdersProjector_Handles(t *testing.T) {
	reg := eventstore.NewRegistry()
	RegisterEvents(reg)
	proj := NewPharmacyOrdersProjector(reg)

	handles := proj.Handles()
	expected := []string{
		"QuoteRequested",
		"QuoteInvitationSent",
		"QuoteInvitationFailed",
		"QuoteReceived",
		"QuoteDeclined",
		"QuoteSelected",
		"QuoteRequestCancelled",
		"QuoteRequestExpired",
		"QuoteRequestClosed",
		"QuoteRequestReopened",
		"OrderPlaced",
		"OrderConfirmed",
		"OrderRejected",
		"OrderPreparing",
		"OrderReady",
		"OrderOutForDelivery",
		"OrderDelivered",
		"OrderCancelled",
		"OrderRated",
	}

	handlesMap := make(map[string]bool)
	for _, h := range handles {
		handlesMap[h] = true
	}

	for _, exp := range expected {
		if !handlesMap[exp] {
			t.Errorf("projector missing expected handle for event: %s", exp)
		}
	}
}
