package infra

import (
	"context"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// TxAuditor implementa app.Auditor escribiendo en platform.audit_log con la
// transacción del UnitOfWork: la entrada y los eventos se confirman juntos.
type TxAuditor struct{}

var _ app.Auditor = TxAuditor{}

// Record inserta la entrada en la tx del contexto.
func (TxAuditor) Record(ctx context.Context, e audit.Entry) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return audit.Record(ctx, tx, e)
}
