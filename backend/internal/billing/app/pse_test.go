package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
)

func pseWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.account.PaymentMethod = &domain.PaymentMethodRef{Provider: payfake.Name, Kind: MethodPSE}
	return w
}

func pseCmd(chargeID uuid.UUID, bank string) PSEPaymentCmd {
	return PSEPaymentCmd{
		ChargeID: chargeID, BankCode: bank, PayerType: providers.PayerCompany, LegalIDType: "NIT", LegalID: "900123456",
		Phone: "573001112233", AcceptanceToken: "acc", ReturnURL: "https://app.example.test/org/x/settings/billing?pse=return",
	}
}

func (w *world) lastFailure(chargeID uuid.UUID) string {
	w.t.Helper()
	code := ""
	for _, e := range w.streams[w.charges[chargeID].SubscriptionID] {
		if f, ok := e.(domain.ChargeFailed); ok && f.ChargeID == chargeID {
			code = f.ErrorCode
		}
	}
	return code
}

// PP-F5-PRD-004: una cuenta con PSE no se cobra sola; el cargo queda a la
// espera del cliente con la gracia de 14 días y paga desde su banco.
func TestCollectCharge_PP_F5_PRD_004_PSEWaitsForPayerThenBankPays(t *testing.T) {
	w := pseWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()

	must(t, w.svc.CollectCharge(ctx, chargeID))
	if w.pay.ChargesCreated() != 0 {
		t.Fatal("PSE no debe crear transacciones sin el pagador")
	}
	if w.charges[chargeID].Status != "failed" || w.lastFailure(chargeID) != PayerActionRequired {
		t.Fatalf("cargo = %s (%s), quiero failed por %s", w.charges[chargeID].Status, w.lastFailure(chargeID), PayerActionRequired)
	}
	if st := w.status(subID); st != domain.StatusGrace {
		t.Fatalf("suscripción = %s, quiero grace", st)
	}

	out, err := w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankApprove))
	must(t, err)
	if out.Status != providers.ChargePaid || out.RedirectURL == "" {
		t.Fatalf("pago = %+v", out)
	}
	if w.charges[chargeID].Status != "paid" || len(w.payments) != 1 || w.status(subID) != domain.StatusActive {
		t.Fatalf("cargo = %s, pagos = %d, suscripción = %s", w.charges[chargeID].Status, len(w.payments), w.status(subID))
	}
	if _, err := w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankApprove)); !errors.Is(err, ErrChargeNotPayable) {
		t.Fatalf("cargo pagado: esperaba ErrChargeNotPayable, obtuvo %v", err)
	}
}

// PP-F5-PRD-004: mientras el pagador está en su banco no se abre otro pago ni el
// dunning lo vuelve a marcar; el webhook lo resuelve.
func TestStartPSEPayment_PP_F5_PRD_004_PendingBlocksSecondPaymentUntilWebhook(t *testing.T) {
	w := pseWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()
	must(t, w.svc.CollectCharge(ctx, chargeID))

	out, err := w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankPending))
	must(t, err)
	if out.Status != providers.ChargePending || out.RedirectURL == "" || w.jobKeys("billing.poll:") != 1 {
		t.Fatalf("pago = %+v, jobs = %+v", out, w.jobs)
	}
	if _, err := w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankApprove)); !errors.Is(err, ErrPaymentInProgress) {
		t.Fatalf("segundo pago: esperaba ErrPaymentInProgress, obtuvo %v", err)
	}
	failures := len(w.streams[subID])
	must(t, w.svc.CollectCharge(ctx, chargeID)) // reintento del dunning
	if len(w.streams[subID]) != failures {
		t.Fatal("el dunning no debe registrar otro fallo con un pago PSE en curso")
	}

	ref := w.attempts[chargeID][0].ProviderRef
	headers, body, err := w.pay.Resolve(ref, providers.ChargePaid, w.now)
	must(t, err)
	if res, err := w.svc.HandleWebhook(ctx, payfake.Name, headers, body); err != nil || res != WebhookProcessed {
		t.Fatalf("webhook = %s, %v", res, err)
	}
	if w.charges[chargeID].Status != "paid" || w.status(subID) != domain.StatusActive || w.pay.ChargesCreated() != 1 {
		t.Fatalf("cargo = %s, suscripción = %s, transacciones = %d", w.charges[chargeID].Status, w.status(subID), w.pay.ChargesCreated())
	}
}

// PP-F5-PRD-004: el banco rechaza; el cliente puede volver a intentarlo con un
// intento nuevo. También sirve para pagar a mano un cargo de tarjeta rechazado.
func TestStartPSEPayment_PP_F5_PRD_004_DeclineAllowsRetryAndCardChargesToo(t *testing.T) {
	w := newWorld(t) // método tarjeta
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()
	w.pay.SetScenario(payfake.ScenarioDeclined)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if w.status(subID) != domain.StatusGrace {
		t.Fatal("la tarjeta rechazada abre la gracia")
	}

	out, err := w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankDecline))
	must(t, err)
	if out.Status != providers.ChargeFailed || w.charges[chargeID].Status != "failed" {
		t.Fatalf("rechazo del banco: pago = %+v, cargo = %s", out, w.charges[chargeID].Status)
	}
	out, err = w.svc.StartPSEPayment(ctx, pseCmd(chargeID, payfake.BankApprove))
	must(t, err)
	if out.Status != providers.ChargePaid || w.status(subID) != domain.StatusActive {
		t.Fatalf("segundo intento: pago = %+v, suscripción = %s", out, w.status(subID))
	}
	if n := len(w.attempts[chargeID]); n != 3 {
		t.Fatalf("intentos = %d, quiero 3 (tarjeta, banco que rechaza, banco que aprueba)", n)
	}
}

func TestStartPSEPayment_PP_F5_PRD_004_Rejections(t *testing.T) {
	ctx := context.Background()

	w := pseWorld(t)
	_, chargeID := w.subscribe(false)
	cmd := pseCmd(chargeID, payfake.BankApprove)
	cmd.LegalID = " "
	if _, err := w.svc.StartPSEPayment(ctx, cmd); !errors.Is(err, ErrPSEPayerIncomplete) {
		t.Fatalf("sin documento: %v", err)
	}
	cmd = pseCmd(chargeID, payfake.BankApprove)
	cmd.Phone = "" // sin teléfono en la cuenta tampoco
	if _, err := w.svc.StartPSEPayment(ctx, cmd); !errors.Is(err, ErrPSEPayerIncomplete) {
		t.Fatalf("sin teléfono: %v", err)
	}
	phone := "573009998877"
	w.account.LegalEntity.Phone = &phone
	if _, err := w.svc.StartPSEPayment(ctx, cmd); err != nil {
		t.Fatalf("el teléfono de la cuenta basta: %v", err)
	}
	if w.pay.ChargesCreated() != 1 {
		t.Fatalf("transacciones = %d, quiero 1", w.pay.ChargesCreated())
	}

	m := pseWorld(t)
	_, manualCharge := m.subscribe(true)
	if _, err := m.svc.StartPSEPayment(ctx, pseCmd(manualCharge, payfake.BankApprove)); !errors.Is(err, ErrChargeNotPayable) {
		t.Fatalf("facturación manual: %v", err)
	}
	if _, err := m.svc.StartPSEPayment(ctx, pseCmd(uuid.New(), payfake.BankApprove)); !errors.Is(err, ErrChargeNotReady) {
		t.Fatalf("cargo desconocido: %v", err)
	}
}
