package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var payT0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// newPaidUpSub devuelve una suscripción Pro de 2 asientos con el cargo inicial pendiente.
func newPendingSub(t *testing.T) (*Subscription, uuid.UUID) {
	t.Helper()
	_, pro, _ := sampleClinicPlans()
	sub, err := Subscribe(uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, payT0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sub.Changes()
	for id := range sub.Charges() {
		return sub, id
	}
	t.Fatal("sin cargo inicial")
	return nil, uuid.Nil
}

func lastEvent(sub *Subscription) any {
	ch := sub.Changes()
	if len(ch) == 0 {
		return nil
	}
	return ch[len(ch)-1]
}

func TestPlanDunning_Schedule(t *testing.T) {
	p := PlanDunning(payT0)
	want := []time.Time{payT0.AddDate(0, 0, 3), payT0.AddDate(0, 0, 7), payT0.AddDate(0, 0, 12)}
	for i, w := range want {
		if !p.Retries[i].Equal(w) {
			t.Fatalf("reintento %d = %s, quiero %s", i, p.Retries[i], w)
		}
	}
	if !p.RestrictAt.Equal(payT0.AddDate(0, 0, 14)) || !p.CancelAt.Equal(payT0.AddDate(0, 0, 60)) {
		t.Fatalf("restricción/cancelación incorrectas: %+v", p)
	}
	if next := p.NextAttempt(payT0.AddDate(0, 0, 4)); next == nil || !next.Equal(want[1]) {
		t.Fatalf("próximo reintento tras d+4 = %v, quiero d+7", next)
	}
	if next := p.NextAttempt(payT0.AddDate(0, 0, 13)); next != nil {
		t.Fatalf("no deben quedar reintentos tras d+12, hay %v", next)
	}
}

func TestSubscription_B4_ChargePaidIsIdempotent(t *testing.T) {
	sub, chargeID := newPendingSub(t)
	if err := sub.RecordChargePaid(chargeID, "wompi", "tx-1", payT0); err != nil {
		t.Fatal(err)
	}
	paid, ok := lastEvent(sub).(ChargePaid)
	if !ok || paid.Provider != "wompi" || paid.PaymentRef != "tx-1" || paid.Manual {
		t.Fatalf("ChargePaid inesperado: %+v", paid)
	}
	// Webhook duplicado y fallo tardío de un intento anterior: sin eventos nuevos.
	if err := sub.RecordChargePaid(chargeID, "wompi", "tx-1", payT0); err != nil {
		t.Fatal(err)
	}
	if err := sub.RecordChargeFailed(chargeID, "DECLINED", payT0); err != nil {
		t.Fatal(err)
	}
	if n := len(sub.Changes()); n != 0 {
		t.Fatalf("resultado repetido emitió %d eventos", n)
	}
	if sub.Status() != StatusActive || sub.Charges()[chargeID].Status != ChargeStatusPaid {
		t.Fatalf("estado alterado: %s / %s", sub.Status(), sub.Charges()[chargeID].Status)
	}
}

func TestSubscription_B4_UnknownChargeIsRejected(t *testing.T) {
	sub, _ := newPendingSub(t)
	unknown := uuid.New()
	for name, err := range map[string]error{
		"paid":     sub.RecordChargePaid(unknown, "wompi", "x", payT0),
		"failed":   sub.RecordChargeFailed(unknown, "X", payT0),
		"restrict": sub.RestrictForNonPayment(unknown, payT0),
		"cancel":   sub.CancelForNonPayment(unknown, payT0),
		"refund":   sub.RefundCharge(unknown, 1, "r", "wompi", "ref", payT0),
		"manual":   sub.RecordManualPayment(unknown, 1, "ref", payT0),
	} {
		if !errors.Is(err, ErrChargeNotFound) {
			t.Errorf("%s: quiero ErrChargeNotFound, obtuve %v", name, err)
		}
	}
}

func TestSubscription_B4_DunningRestrictsOnlyFromGraceWithFailedCharge(t *testing.T) {
	sub, chargeID := newPendingSub(t)

	// Activa y sin fallo: el job de restricción no hace nada.
	if err := sub.RestrictForNonPayment(chargeID, payT0); err != nil || sub.Status() != StatusActive {
		t.Fatalf("restricción sin impago: err=%v estado=%s", err, sub.Status())
	}

	_ = sub.RecordChargeFailed(chargeID, "DECLINED", payT0)
	if sub.Status() != StatusGrace {
		t.Fatalf("primer fallo debe abrir gracia, estado=%s", sub.Status())
	}
	if ends := sub.Dunning().GraceEndsAt; ends == nil || !ends.Equal(payT0.AddDate(0, 0, DunningGraceDays)) {
		t.Fatalf("fin de gracia = %v", ends)
	}

	d14 := payT0.AddDate(0, 0, 14)
	if err := sub.RestrictForNonPayment(chargeID, d14); err != nil || sub.Status() != StatusRestricted {
		t.Fatalf("d+14: err=%v estado=%s", err, sub.Status())
	}
	sub.Changes()
	// Repetido: idempotente.
	_ = sub.RestrictForNonPayment(chargeID, d14)
	if n := len(sub.Changes()); n != 0 {
		t.Fatalf("restricción repetida emitió %d eventos", n)
	}

	d60 := payT0.AddDate(0, 0, 60)
	if err := sub.CancelForNonPayment(chargeID, d60); err != nil || sub.Status() != StatusCancelled {
		t.Fatalf("d+60: err=%v estado=%s", err, sub.Status())
	}
	if at := sub.CancelledAt(); at == nil || !at.Equal(d60) {
		t.Fatalf("cancelled_at = %v", at)
	}
	sub.Changes()
	_ = sub.CancelForNonPayment(chargeID, d60)
	if n := len(sub.Changes()); n != 0 {
		t.Fatalf("cancelación repetida emitió %d eventos", n)
	}
}

func TestSubscription_B4_PaymentDuringRestrictionReactivates(t *testing.T) {
	sub, chargeID := newPendingSub(t)
	_ = sub.RecordChargeFailed(chargeID, "DECLINED", payT0)
	_ = sub.RestrictForNonPayment(chargeID, payT0.AddDate(0, 0, 14))

	if err := sub.RecordChargePaid(chargeID, "wompi", "tx-2", payT0.AddDate(0, 0, 20)); err != nil {
		t.Fatal(err)
	}
	if sub.Status() != StatusActive || sub.Dunning() != nil {
		t.Fatalf("pago no reactivó: estado=%s dunning=%v", sub.Status(), sub.Dunning())
	}
	// El job de cancelación d+60 llega después del pago: no cancela.
	_ = sub.CancelForNonPayment(chargeID, payT0.AddDate(0, 0, 60))
	if sub.Status() != StatusActive {
		t.Fatalf("cancelación tras pago: estado=%s", sub.Status())
	}
}

func TestSubscription_B4_ReactivationWaitsForAllFailedCharges(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sub, first := newPendingSub(t)
	_ = sub.RecordChargePaid(first, "wompi", "tx-1", payT0)
	// Segundo cargo: prorrateo por asiento adicional.
	if err := sub.AdjustSeats(3, pro, 1, 0.19, payT0.AddDate(0, 0, 5)); err != nil {
		t.Fatal(err)
	}
	// Tercer cargo: renovación.
	if err := sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	var failed []uuid.UUID
	for id, ch := range sub.Charges() {
		if ch.Status == ChargeStatusPending {
			failed = append(failed, id)
			_ = sub.RecordChargeFailed(id, "DECLINED", payT0.AddDate(0, 1, 0))
		}
	}
	if len(failed) != 2 || sub.Status() != StatusGrace {
		t.Fatalf("preparación: fallidos=%d estado=%s", len(failed), sub.Status())
	}
	_ = sub.RecordChargePaid(failed[0], "wompi", "tx-a", payT0.AddDate(0, 1, 1))
	if sub.Status() != StatusGrace || sub.Dunning() == nil {
		t.Fatalf("con un cargo aún fallido debe seguir en gracia, estado=%s", sub.Status())
	}
	_ = sub.RecordChargePaid(failed[1], "wompi", "tx-b", payT0.AddDate(0, 1, 1))
	if sub.Status() != StatusActive || sub.Dunning() != nil {
		t.Fatalf("al pagar todo debe reactivarse, estado=%s", sub.Status())
	}
}

func TestSubscription_ManualPayment_MustMatchTotal(t *testing.T) {
	sub, chargeID := newPendingSub(t)
	total := sub.Charges()[chargeID].Total
	if err := sub.RecordManualPayment(chargeID, total-1, "TRF-1", payT0); !errors.Is(err, ErrManualAmountMismatch) {
		t.Fatalf("quiero ErrManualAmountMismatch, obtuve %v", err)
	}
	// También regulariza un cargo fallido y reactiva.
	_ = sub.RecordChargeFailed(chargeID, "DECLINED", payT0)
	if err := sub.RecordManualPayment(chargeID, total, "TRF-1", payT0.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	if sub.Status() != StatusActive {
		t.Fatalf("pago manual no reactivó: %s", sub.Status())
	}
	evs := sub.Changes()
	var paid ChargePaid
	for _, e := range evs {
		if p, ok := e.(ChargePaid); ok {
			paid = p
		}
	}
	if !paid.Manual || paid.Provider != "manual" || paid.Amount != total {
		t.Fatalf("ChargePaid manual inesperado: %+v", paid)
	}
}

func TestSubscription_B8_RefundChargeCarriesProviderRef(t *testing.T) {
	sub, chargeID := newPendingSub(t)
	if err := sub.CheckRefundable(chargeID, 1); !errors.Is(err, ErrChargeNotPaid) {
		t.Fatalf("cargo pendiente: quiero ErrChargeNotPaid, obtuve %v", err)
	}
	_ = sub.RecordChargePaid(chargeID, "wompi", "tx-1", payT0)
	total := sub.Charges()[chargeID].Total
	if err := sub.CheckRefundable(chargeID, total+0.01); !errors.Is(err, ErrRefundExceeds) {
		t.Fatalf("quiero ErrRefundExceeds, obtuve %v", err)
	}
	if err := sub.CheckRefundable(chargeID, 0); !errors.Is(err, ErrRefundExceeds) {
		t.Fatalf("importe cero: quiero ErrRefundExceeds, obtuve %v", err)
	}
	sub.Changes()
	if err := sub.RefundCharge(chargeID, 1000, "cortesía", "wompi", "rf-1", payT0); err != nil {
		t.Fatal(err)
	}
	ev, ok := lastEvent(sub).(ChargeRefunded)
	if !ok || ev.RefundRef != "rf-1" || ev.Provider != "wompi" || ev.Amount != 1000 {
		t.Fatalf("ChargeRefunded inesperado: %+v", ev)
	}
	if err := sub.RefundCharge(chargeID, total, "excede", "wompi", "rf-2", payT0); !errors.Is(err, ErrRefundExceeds) {
		t.Fatalf("B8: quiero ErrRefundExceeds, obtuve %v", err)
	}
}

func TestSubscription_AdminAdjustment_ExtendTrial(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sub, _ := StartTrial(uuid.New(), uuid.New(), KindClinic, pro, payT0)
	end := *sub.TrialEndsAt()

	if err := sub.ExtendTrial(end, "igual", payT0); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("fecha no posterior: obtuve %v", err)
	}
	newEnd := end.AddDate(0, 0, 15)
	if err := sub.ExtendTrial(newEnd, "piloto ampliado", payT0); err != nil {
		t.Fatal(err)
	}
	if !sub.TrialEndsAt().Equal(newEnd) || !sub.CurrentPeriod().End.Equal(newEnd) {
		t.Fatalf("prueba no ampliada: %v", sub.TrialEndsAt())
	}

	// Prueba vencida (restringida) → ampliar la devuelve a trialing.
	_ = sub.ExpireTrial(newEnd.Add(time.Hour))
	later := newEnd.AddDate(0, 0, 10)
	if err := sub.ExtendTrial(later, "segunda oportunidad", newEnd.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if sub.Status() != StatusTrialing {
		t.Fatalf("estado tras ampliar prueba vencida = %s", sub.Status())
	}

	// Una suscripción contratada no admite ampliar prueba.
	paidSub, _ := newPendingSub(t)
	if err := paidSub.ExtendTrial(later, "x", payT0); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("suscripción contratada: obtuve %v", err)
	}
}

func TestSubscription_AdminAdjustment_CreditReducesNextRenewals(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sub, first := newPendingSub(t)
	_ = sub.RecordChargePaid(first, "wompi", "tx-1", payT0)

	if err := sub.GrantCredit(0, "nada", payT0); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("crédito cero: obtuve %v", err)
	}
	// 2 asientos × 75.000 = 150.000; crédito de 200.000 cubre una renovación y parte de la siguiente.
	if err := sub.GrantCredit(200000, "compensación por incidente", payT0); err != nil {
		t.Fatal(err)
	}
	sub.Changes()

	_ = sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 1, 0))
	ch1 := lastEvent(sub).(ChargeScheduled)
	if ch1.Amount != 0 || ch1.Tax != 0 || ch1.Total != 0 || ch1.CreditApplied != 150000 {
		t.Fatalf("primera renovación: %+v", ch1)
	}
	if sub.CreditBalance() != 50000 {
		t.Fatalf("saldo de crédito = %.2f, quiero 50000", sub.CreditBalance())
	}

	_ = sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 2, 0))
	ch2 := lastEvent(sub).(ChargeScheduled)
	if ch2.Amount != 100000 || ch2.Tax != 19000 || ch2.Total != 119000 || ch2.CreditApplied != 50000 {
		t.Fatalf("segunda renovación: %+v", ch2)
	}
	if sub.CreditBalance() != 0 {
		t.Fatalf("saldo de crédito = %.2f, quiero 0", sub.CreditBalance())
	}
	var sum float64
	for _, l := range ch2.Lines {
		sum += l.Amount
	}
	if RoundCents(sum) != ch2.Amount {
		t.Fatalf("Σ líneas %.2f ≠ subtotal %.2f", sum, ch2.Amount)
	}
}

func TestSubscription_Coupon_AppliesUntilExpiry(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sub, first := newPendingSub(t)
	_ = sub.RecordChargePaid(first, "wompi", "tx-1", payT0)

	until := payT0.AddDate(0, 2, 0) // cubre la renovación que empieza en +1 mes, no la de +2
	bad := []CouponApplied{
		{Code: "X", Kind: "pct", Value: 120, Until: &until},
		{Code: "X", Kind: "amount", Value: 0, Until: &until},
		{Code: "X", Kind: "otro", Value: 10, Until: &until},
		{Code: "X", Kind: "pct", Value: 10},
	}
	for _, c := range bad {
		if err := sub.ApplyCoupon(c, payT0); !errors.Is(err, ErrInvalidAdjustment) {
			t.Fatalf("cupón %+v: obtuve %v", c, err)
		}
	}
	if err := sub.ApplyCoupon(CouponApplied{Code: "COMP", Kind: "pct", Value: 100, Until: &until}, payT0); err != nil {
		t.Fatal(err)
	}
	sub.Changes()

	_ = sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 1, 0))
	if ch := lastEvent(sub).(ChargeScheduled); ch.Total != 0 {
		t.Fatalf("renovación con cortesía del 100 %%: total=%.2f", ch.Total)
	}
	_ = sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 2, 0))
	if ch := lastEvent(sub).(ChargeScheduled); ch.Total != 178500 {
		t.Fatalf("renovación con cupón vencido: total=%.2f, quiero 178500", ch.Total)
	}

	// Cupón de importe fijo, acotado al subtotal.
	until2 := payT0.AddDate(1, 0, 0)
	_ = sub.ApplyCoupon(CouponApplied{Code: "FIJO", Kind: "amount", Value: 50000, Until: &until2}, payT0.AddDate(0, 2, 0))
	sub.Changes()
	_ = sub.RenewPeriod(pro, 1, 0.19, payT0.AddDate(0, 3, 0))
	if ch := lastEvent(sub).(ChargeScheduled); ch.Amount != 100000 || ch.Total != 119000 {
		t.Fatalf("cupón de importe: %+v", ch)
	}
}

func TestSubscription_AdjustmentsRejectedWhenCancelled(t *testing.T) {
	sub, _ := newPendingSub(t)
	_ = sub.Cancel(false, "baja", payT0)
	until := payT0.AddDate(0, 1, 0)
	if err := sub.GrantCredit(100, "x", payT0); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("crédito en cancelada: %v", err)
	}
	if err := sub.ApplyCoupon(CouponApplied{Code: "C", Kind: "pct", Value: 10, Until: &until}, payT0); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("cupón en cancelada: %v", err)
	}
}

// Rehidratar desde los eventos emitidos reproduce el mismo estado (ES).
func TestSubscription_RehydrationPreservesPaymentState(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sub, err := Subscribe(uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, payT0)
	if err != nil {
		t.Fatal(err)
	}
	var chargeID uuid.UUID
	for id := range sub.Charges() {
		chargeID = id
	}
	_ = sub.RecordChargeFailed(chargeID, "DECLINED", payT0)
	_ = sub.GrantCredit(30000, "cortesía", payT0)
	_ = sub.RestrictForNonPayment(chargeID, payT0.AddDate(0, 0, 14))

	replayed := NewSubscription()
	for _, e := range sub.Changes() {
		replayed.Apply(e)
	}
	if replayed.Status() != StatusRestricted || replayed.CreditBalance() != 30000 ||
		replayed.Dunning() == nil || replayed.Dunning().Attempts != 1 ||
		replayed.Charges()[chargeID].Status != ChargeStatusFailed {
		t.Fatalf("estado rehidratado distinto: %s crédito=%.2f dunning=%+v", replayed.Status(), replayed.CreditBalance(), replayed.Dunning())
	}
}
