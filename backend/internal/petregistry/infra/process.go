package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
)

// ProcessExpireCareLinkRequests programa el vencimiento (14 días) de cada
// solicitud de vínculo y lo cancela cuando se resuelve (PP-F2-PRD-020, 02 §7).
const ProcessExpireCareLinkRequests = "petregistry.expire-care-link-requests"

// ExpireCareLinkRequestKey es la clave de deduplicación del job.
func ExpireCareLinkRequestKey(requestID uuid.UUID) string {
	return "petregistry.expire-care-link-request:" + requestID.String()
}

// CareLinkRequestTimers implementa process.ProcessManager. Nunca llama al bus:
// solo programa/cancela el comando del sistema ExpireCareLinkRequest.
type CareLinkRequestTimers struct{ jobs process.Jobs }

var _ process.ProcessManager = (*CareLinkRequestTimers)(nil)

// NewCareLinkRequestTimers construye el PM.
func NewCareLinkRequestTimers(jobs process.Jobs) *CareLinkRequestTimers {
	return &CareLinkRequestTimers{jobs: jobs}
}

// Name implementa ProcessManager.
func (*CareLinkRequestTimers) Name() string { return ProcessExpireCareLinkRequests }

// Handles implementa ProcessManager.
func (*CareLinkRequestTimers) Handles() []string {
	return []string{"CareLinkRequested", "CareLinkRequestApproved", "CareLinkRequestDeclined", "CareLinkRequestExpired"}
}

// Emits implementa ProcessManager.
func (*CareLinkRequestTimers) Emits() []string {
	return []string{domain.ExpireCareLinkRequest{}.CommandName()}
}

// Handle implementa ProcessManager.
func (p *CareLinkRequestTimers) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	// CareLinkRequested trae expires_at; los demás eventos solo el request_id.
	var ev domain.CareLinkRequested
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return fmt.Errorf("%s: payload de %s: %w", ProcessExpireCareLinkRequests, e.Type, err)
	}
	if e.Type != "CareLinkRequested" {
		return p.jobs.Cancel(ctx, tx, ExpireCareLinkRequestKey(ev.RequestID))
	}
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, ev.ExpiresAt,
		domain.ExpireCareLinkRequest{PetID: ev.PetID, RequestID: ev.RequestID},
		ExpireCareLinkRequestKey(ev.RequestID)))
}
