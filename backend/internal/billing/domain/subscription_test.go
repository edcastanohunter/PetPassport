package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func sampleClinicPlans() (PlanDefinition, PlanDefinition, PlanDefinition) {
	starter := PlanDefinition{
		Code:    "starter",
		Version: 1,
		Kind:    "clinic",
		Pricing: PlanPricing{
			Model:    "flat",
			Currency: "COP",
			Monthly:  120000.00,
			Annual:   1200000.00,
		},
		TrialDays: 0,
		Seats:     &PlanSeatsRange{Min: 1, Max: ptrInt(2)},
		Quotas:    map[string]int64{"quotes_per_month": 50},
		Features:  []string{"agenda", "encounters", "prescriptions"},
		Status:    "published",
	}

	pro := PlanDefinition{
		Code:    "pro",
		Version: 1,
		Kind:    "clinic",
		Pricing: PlanPricing{
			Model:    "per_seat",
			Currency: "COP",
			Monthly:  75000.00,
			Annual:   750000.00,
		},
		TrialDays: 30,
		Seats:     &PlanSeatsRange{Min: 1},
		Quotas:    map[string]int64{"quotes_per_month": 500, "sms_per_month": 100},
		Features:  []string{"agenda", "encounters", "prescriptions", "cohorts", "directory", "sms"},
		Status:    "published",
	}

	enterprise := PlanDefinition{
		Code:    "enterprise",
		Version: 1,
		Kind:    "clinic",
		Pricing: PlanPricing{
			Model:    "per_seat",
			Currency: "COP",
			Monthly:  150000.00,
			Annual:   1500000.00,
		},
		TrialDays: 0,
		Seats:     &PlanSeatsRange{Min: 5},
		Quotas:    map[string]int64{"quotes_per_month": 10000},
		Features:  []string{"agenda", "encounters", "prescriptions", "multi_org", "sla_999"},
		Status:    "published",
	}

	return starter, pro, enterprise
}

func samplePharmacyPlan() PlanDefinition {
	return PlanDefinition{
		Code:    "pharmacy_pro",
		Version: 1,
		Kind:    "pharmacy",
		Pricing: PlanPricing{
			Model:    "tiered",
			Currency: "COP",
			Monthly:  120000.00,
			Annual:   1200000.00,
			Tiers: []VolumeTier{
				{Code: "tier_200", UpTo: 200, Price: 120000.00},
				{Code: "tier_1000", UpTo: 1000, Price: 350000.00},
				{Code: "tier_unlimited", UpTo: -1, Price: 600000.00},
			},
		},
		TrialDays: 30,
		Quotas:    map[string]int64{"quotes_per_month": 200},
		Features:  []string{"quote_portal", "orders", "partner_api"},
		Status:    "published",
	}
}

func ptrInt(i int) *int              { return &i }
func ptrTime(t time.Time) *time.Time { return &t }

// Invariante B1: Una suscripción por cuenta y tipo
func TestSubscription_B1_OnePerAccountAndKind(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	subID := uuid.New()
	accountID := uuid.New()

	sub, err := StartTrial(subID, accountID, KindClinic, pro, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.Kind() != KindClinic || sub.AccountID() != accountID {
		t.Fatalf("unexpected sub state: kind=%s, accountID=%s", sub.Kind(), sub.AccountID())
	}
}

// Invariante B2: Método de pago salvo manual_billing
func TestSubscription_B2_PaymentMethodRequired(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// Falla si no es manual y no tiene método de pago
	_, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, // manualBilling
		false, // hasPaymentMethod
		1, 0.19, now,
	)
	if err != ErrPaymentMethodRequired {
		t.Fatalf("expected ErrPaymentMethodRequired, got %v", err)
	}

	// Éxito con método de pago
	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, // manualBilling
		true,  // hasPaymentMethod
		1, 0.19, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.Status() != StatusActive {
		t.Fatalf("expected status active, got %s", sub.Status())
	}

	// Éxito sin tarjeta si es manualBilling
	subManual, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		true,  // manualBilling
		false, // hasPaymentMethod
		1, 0.19, now,
	)
	if err != nil {
		t.Fatalf("unexpected error with manual billing: %v", err)
	}
	if !subManual.ManualBilling() {
		t.Fatal("expected ManualBilling true")
	}
}

// Invariante B3: Asientos >= profesionales activos
func TestSubscription_B3_SeatsAboveOrEqualActiveProfessionals(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// Intento de contratar 2 asientos con 3 profesionales activos
	_, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, true,
		3, // activeProfessionals
		0.19, now,
	)
	if err != ErrSeatsBelowUsage {
		t.Fatalf("expected ErrSeatsBelowUsage, got %v", err)
	}

	// Contratar 3 asientos con 3 profesionales activos
	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 3, nil, CycleMonthly, nil,
		false, true,
		3, 0.19, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Intento de reducir a 2 asientos cuando hay 3 profesionales
	err = sub.AdjustSeats(2, pro, 3, 0.19, now)
	if err != ErrSeatsBelowUsage {
		t.Fatalf("expected ErrSeatsBelowUsage al reducir, got %v", err)
	}

	// Reducir a 3 es no-op (delta 0)
	if err := sub.AdjustSeats(3, pro, 3, 0.19, now); err != nil {
		t.Fatalf("unexpected error on no-op adjust: %v", err)
	}
}

// Invariante B4: Transiciones de estado válidas y reactivación al pagar
func TestSubscription_B4_ValidStateTransitions(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	sim := kernel.NewSimClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))

	sub, err := StartTrial(uuid.New(), uuid.New(), KindClinic, pro, sim.Now())
	if err != nil {
		t.Fatalf("start trial error: %v", err)
	}
	if sub.Status() != StatusTrialing {
		t.Fatalf("expected trialing, got %s", sub.Status())
	}

	// trialing -> active
	err = sub.ConvertTrialToSubscription(pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, sim.Now())
	if err != nil {
		t.Fatalf("convert trial error: %v", err)
	}
	if sub.Status() != StatusActive {
		t.Fatalf("expected active, got %s", sub.Status())
	}

	// Buscar el cargo pendiente y simular cobro fallido -> grace (14 días)
	var pendingChargeID uuid.UUID
	for id, ch := range sub.Charges() {
		if ch.Status == ChargeStatusPending {
			pendingChargeID = id
			break
		}
	}

	err = sub.RecordChargeResult(pendingChargeID, "failed", "", "CARD_DECLINED", sim.Now())
	if err != nil {
		t.Fatalf("record charge failed error: %v", err)
	}
	if sub.Status() != StatusGrace {
		t.Fatalf("expected grace, got %s", sub.Status())
	}
	if sub.Dunning() == nil || sub.Dunning().Attempts != 1 {
		t.Fatalf("expected dunning attempts 1")
	}

	// Simular avance de 14 días y restricción -> restricted
	sim.Advance(14 * 24 * time.Hour)
	err = sub.Restrict("Gracia de 14 días agotada", sim.Now())
	if err != nil {
		t.Fatalf("restrict error: %v", err)
	}
	if sub.Status() != StatusRestricted {
		t.Fatalf("expected restricted, got %s", sub.Status())
	}

	// restricted -> active tras pago exitoso
	err = sub.RecordChargeResult(pendingChargeID, "paid", "txn_reintent_ok", "", sim.Now())
	if err != nil {
		t.Fatalf("record paid error: %v", err)
	}
	if sub.Status() != StatusActive {
		t.Fatalf("expected active tras pago, got %s", sub.Status())
	}
	if sub.Dunning() != nil {
		t.Fatal("expected dunning reset")
	}
}

// Invariante B5: Prorrateo = (días restantes / días periodo) * diferencia al céntimo
func TestSubscription_B5_ProrationFormula(t *testing.T) {
	starter, pro, _ := sampleClinicPlans()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	// Suscribir starter mensual (120,000 COP) por 30 días (hasta 1 dic)
	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, starter, 1, nil, CycleMonthly, nil,
		false, true, 1, 0.19, start,
	)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	// Marcar el cargo inicial pagado
	var initialChargeID uuid.UUID
	for id := range sub.Charges() {
		initialChargeID = id
	}
	_ = sub.RecordChargeResult(initialChargeID, "paid", "txn_initial", "", start)

	// Upgrade a pro (2 asientos = 150,000 COP) el 11 de noviembre (quedan 20 días de 30)
	upgradeTime := start.AddDate(0, 0, 10)
	err = sub.ChangePlan(pro, 2, nil, CycleMonthly, starter, 1, 0.19, upgradeTime)
	if err != nil {
		t.Fatalf("change plan error: %v", err)
	}

	// Diferencia = 150,000 - 120,000 = 30,000
	// Prorrateo = (20 / 30) * 30,000 = 20,000 COP
	// Tax (19%) = 3,800 COP
	// Total = 23,800 COP
	var prorationCharge *Charge
	for _, ch := range sub.Charges() {
		if ch.ID != initialChargeID {
			c := ch
			prorationCharge = &c
			break
		}
	}
	if prorationCharge == nil {
		t.Fatal("expected proration charge scheduled")
	}
	if prorationCharge.Amount != 20000.00 {
		t.Fatalf("expected proration amount 20000.00, got %.2f", prorationCharge.Amount)
	}
	if prorationCharge.Tax != 3800.00 {
		t.Fatalf("expected proration tax 3800.00, got %.2f", prorationCharge.Tax)
	}
	if prorationCharge.Total != 23800.00 {
		t.Fatalf("expected proration total 23800.00, got %.2f", prorationCharge.Total)
	}
}

// Invariante B6: Importes calculados por el agregado desde el plan versionado
func TestSubscription_B6_AmountsCalculatedFromVersionedPlan(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// pro: 75,000 por asiento * 4 asientos = 300,000
	// IVA 19% = 57,000
	// Total = 357,000
	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 4, nil, CycleMonthly, nil,
		false, true, 2, 0.19, now,
	)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	for _, ch := range sub.Charges() {
		if ch.Amount != 300000.00 {
			t.Fatalf("expected 300000.00, got %.2f", ch.Amount)
		}
		if ch.Tax != 57000.00 {
			t.Fatalf("expected 57000.00, got %.2f", ch.Tax)
		}
		if ch.Total != 357000.00 {
			t.Fatalf("expected 357000.00, got %.2f", ch.Total)
		}
	}
}

// Invariante B7: Un cargo por periodo (+ cargos de prorrateo)
func TestSubscription_B7_OneChargePerPeriodAndProrationCharges(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, true, 1, 0.19, start,
	)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	if len(sub.Charges()) != 1 {
		t.Fatalf("expected 1 charge initially, got %d", len(sub.Charges()))
	}

	// Adición de asientos en el día 15 -> genera exactamente 1 cargo de prorrateo
	day15 := start.AddDate(0, 0, 15)
	err = sub.AdjustSeats(3, pro, 1, 0.19, day15)
	if err != nil {
		t.Fatalf("adjust seats error: %v", err)
	}
	if len(sub.Charges()) != 2 {
		t.Fatalf("expected 2 charges after proration, got %d", len(sub.Charges()))
	}

	// Renovación al fin de periodo -> genera 1 cargo del nuevo periodo
	end := start.AddDate(0, 1, 0)
	err = sub.RenewPeriod(pro, 1, 0.19, end)
	if err != nil {
		t.Fatalf("renew period error: %v", err)
	}
	if len(sub.Charges()) != 3 {
		t.Fatalf("expected 3 charges after renewal, got %d", len(sub.Charges()))
	}
}

// Invariante B8: Refund <= Total - Refunded
func TestSubscription_B8_RefundMustNotExceedTotalMinusRefunded(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 1, nil, CycleMonthly, nil,
		false, true, 1, 0.19, now,
	)

	var chargeID uuid.UUID
	var chargeTotal float64
	for id, ch := range sub.Charges() {
		chargeID = id
		chargeTotal = ch.Total
	}

	// No se puede reembolsar antes de pagar
	if err := sub.Refund(chargeID, 10000, "test", now); err != ErrChargeNotPaid {
		t.Fatalf("expected ErrChargeNotPaid, got %v", err)
	}

	_ = sub.RecordChargeResult(chargeID, "paid", "txn_123", "", now)

	// Reembolso mayor al total
	if err := sub.Refund(chargeID, chargeTotal+1, "test", now); err != ErrRefundExceeds {
		t.Fatalf("expected ErrRefundExceeds, got %v", err)
	}

	// Reembolso parcial
	part1 := 20000.00
	if err := sub.Refund(chargeID, part1, "parcial 1", now); err != nil {
		t.Fatalf("unexpected error on partial refund: %v", err)
	}
	if sub.Charges()[chargeID].Refunded != part1 {
		t.Fatalf("expected refunded %.2f, got %.2f", part1, sub.Charges()[chargeID].Refunded)
	}
	if sub.Charges()[chargeID].Status != ChargeStatusPartiallyRefunded {
		t.Fatalf("expected status partially_refunded, got %s", sub.Charges()[chargeID].Status)
	}

	// Segundo reembolso por el resto
	remaining := chargeTotal - part1
	if err := sub.Refund(chargeID, remaining, "parcial 2", now); err != nil {
		t.Fatalf("unexpected error on second refund: %v", err)
	}
	if sub.Charges()[chargeID].Status != ChargeStatusRefunded {
		t.Fatalf("expected status refunded, got %s", sub.Charges()[chargeID].Status)
	}

	// Tercer reembolso debe fallar
	if err := sub.Refund(chargeID, 1.00, "excede", now); err != ErrRefundExceeds {
		t.Fatalf("expected ErrRefundExceeds, got %v", err)
	}
}

// Invariante B9: Cancelación al fin de periodo mantiene entitlements hasta Period.End
func TestSubscription_B9_CancelAtPeriodEndMaintainsEntitlements(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, true, 1, 0.19, start,
	)

	// Cancelar al fin de periodo
	err := sub.Cancel(true, "Cambio de software", start.AddDate(0, 0, 5))
	if err != nil {
		t.Fatalf("cancel error: %v", err)
	}

	if !sub.CancelAtPeriodEnd() {
		t.Fatal("expected cancelAtPeriodEnd true")
	}
	if sub.Status() != StatusActive {
		t.Fatalf("expected status still active until end, got %s", sub.Status())
	}

	// Reanudar suscripción (deshacer cancelación programada)
	if err := sub.Resume(start.AddDate(0, 0, 10)); err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if sub.CancelAtPeriodEnd() {
		t.Fatal("expected cancelAtPeriodEnd false after resume")
	}

	// Cancelar de nuevo y simular llegada al final de periodo -> debe pasar a cancelled
	_ = sub.Cancel(true, "Cancelación definitiva", start.AddDate(0, 0, 15))
	periodEnd := start.AddDate(0, 1, 0)
	err = sub.RenewPeriod(pro, 1, 0.19, periodEnd)
	if err != nil {
		t.Fatalf("renew error: %v", err)
	}
	if sub.Status() != StatusCancelled {
		t.Fatalf("expected status cancelled at period end, got %s", sub.Status())
	}
}

// Invariante B10: PlanVersion fijado al suscribir; cambios de precio aplican con >= 30 días de antelación
func TestSubscription_B10_PlanVersionFixedAnd30DaysPriceChangeRule(t *testing.T) {
	_, proV1, _ := sampleClinicPlans()
	proV1.PublishedAt = ptrTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, proV1, 2, nil, CycleMonthly, nil,
		false, true, 1, 0.19, start,
	)
	if sub.PlanVersion() != 1 {
		t.Fatalf("expected version 1, got %d", sub.PlanVersion())
	}

	// Nueva versión v2 con precio más alto publicada el 15 de noviembre (hace solo 15 días respecto a 1 dic)
	proV2 := proV1
	proV2.Version = 2
	proV2.Pricing.Monthly = 85000.00
	proV2.PublishedAt = ptrTime(time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))

	// Renovar el 1 de diciembre: como se publicó hace < 30 días, NO aplica v2 aún (RN-39)
	dec1 := start.AddDate(0, 1, 0)
	err := sub.RenewPeriod(proV2, 1, 0.19, dec1)
	if err != nil {
		t.Fatalf("renew error: %v", err)
	}
	if sub.PlanVersion() != 1 {
		t.Fatalf("expected version still 1 because < 30 days notice, got %d", sub.PlanVersion())
	}

	// Si se renueva el 1 de enero (> 30 días desde 15 nov), ahora SÍ aplica v2
	jan1 := dec1.AddDate(0, 1, 0)
	err = sub.RenewPeriod(proV2, 1, 0.19, jan1)
	if err != nil {
		t.Fatalf("renew error: %v", err)
	}
	if sub.PlanVersion() != 2 {
		t.Fatalf("expected version 2 after >= 30 days notice, got %d", sub.PlanVersion())
	}
}

// Prueba con reloj simulado para el ciclo completo de dunning (QA-001)
func TestSubscription_DunningSimulatedClock(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	clock := kernel.NewSimClock(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))

	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, true, 1, 0.19, clock.Now(),
	)

	var chargeID uuid.UUID
	for id := range sub.Charges() {
		chargeID = id
	}

	// d0: fallo de cobro -> pasa a grace
	_ = sub.RecordChargeResult(chargeID, "failed", "", "EXPIRED_CARD", clock.Now())
	if sub.Status() != StatusGrace {
		t.Fatalf("d0: expected grace, got %s", sub.Status())
	}

	// d+3: reintento 2 fallido
	clock.Advance(3 * 24 * time.Hour)
	_ = sub.RecordChargeResult(chargeID, "failed", "", "EXPIRED_CARD", clock.Now())
	if sub.Dunning().Attempts != 2 {
		t.Fatalf("d+3: expected 2 attempts, got %d", sub.Dunning().Attempts)
	}

	// d+7: reintento 3 fallido
	clock.Advance(4 * 24 * time.Hour)
	_ = sub.RecordChargeResult(chargeID, "failed", "", "EXPIRED_CARD", clock.Now())

	// d+14: expira periodo de gracia -> restrict
	clock.Advance(7 * 24 * time.Hour)
	_ = sub.Restrict("14 días de gracia agotados", clock.Now())
	if sub.Status() != StatusRestricted {
		t.Fatalf("d+14: expected restricted, got %s", sub.Status())
	}

	// d+60: cancelación definitiva tras impago prolongado
	clock.Advance(46 * 24 * time.Hour)
	_ = sub.Cancel(false, "Cancelación dunning d+60", clock.Now())
	if sub.Status() != StatusCancelled {
		t.Fatalf("d+60: expected cancelled, got %s", sub.Status())
	}
}

// Prueba con reloj simulado para prueba de 30 días y vencimiento (PRD-002)
func TestSubscription_TrialExpirySimulatedClock(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	clock := kernel.NewSimClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	sub, _ := StartTrial(uuid.New(), uuid.New(), KindClinic, pro, clock.Now())
	if sub.Status() != StatusTrialing {
		t.Fatalf("expected trialing, got %s", sub.Status())
	}

	// Día 30: expira la prueba
	clock.Advance(30 * 24 * time.Hour)
	err := sub.ExpireTrial(clock.Now())
	if err != nil {
		t.Fatalf("expire trial error: %v", err)
	}
	if sub.Status() != StatusRestricted {
		t.Fatalf("expected restricted after trial expiration, got %s", sub.Status())
	}
}

// Prueba de cupones y descuentos porcentuales y fijos (COULD PRD-022)
func TestSubscription_Coupons(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// Cupón 20%
	coupon20 := &CouponApplied{Code: "DESC20", Kind: "pct", Value: 20}
	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, coupon20,
		false, true, 1, 0.19, now,
	)
	// Subtotal sin cupón: 150,000. Con 20%: 120,000. IVA 19%: 22,800. Total: 142,800
	for _, ch := range sub.Charges() {
		if ch.Amount != 120000.00 {
			t.Fatalf("expected subtotal 120000.00, got %.2f", ch.Amount)
		}
		if ch.Total != 142800.00 {
			t.Fatalf("expected total 142800.00, got %.2f", ch.Total)
		}
	}

	// Cupón monto fijo 50,000
	coupon50k := &CouponApplied{Code: "DESC50K", Kind: "amount", Value: 50000}
	sub2, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, coupon50k,
		false, true, 1, 0.19, now,
	)
	// Subtotal sin cupón: 150,000. Con -50,000: 100,000. IVA 19%: 19,000. Total: 119,000
	for _, ch := range sub2.Charges() {
		if ch.Amount != 100000.00 {
			t.Fatalf("expected subtotal 100000.00, got %.2f", ch.Amount)
		}
		if ch.Total != 119000.00 {
			t.Fatalf("expected total 119000.00, got %.2f", ch.Total)
		}
	}
}

// Prueba de suscripción y tiers para farmacias
func TestSubscription_PharmacyTiered(t *testing.T) {
	pharma := samplePharmacyPlan()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	tierCode := "tier_1000"
	sub, err := Subscribe(
		uuid.New(), uuid.New(), KindPharmacy, pharma, 0, &tierCode, CycleMonthly, nil,
		false, true, 0, 0.19, now,
	)
	if err != nil {
		t.Fatalf("pharmacy subscribe error: %v", err)
	}

	// tier_1000 = 350,000 COP mensual. IVA 19% = 66,500. Total: 416,500
	for _, ch := range sub.Charges() {
		if ch.Amount != 350000.00 {
			t.Fatalf("expected 350000.00, got %.2f", ch.Amount)
		}
		if ch.Total != 416500.00 {
			t.Fatalf("expected 416500.00, got %.2f", ch.Total)
		}
	}
}
