package domain

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// Subscription representa una suscripción a un plan de clínica o farmacia (PP-F5-DOM-004).
// Stream ID: sub-<id>.
type Subscription struct {
	id                uuid.UUID
	version           int64
	accountID         uuid.UUID
	kind              SubscriptionKind
	planCode          string
	planVersion       int
	seats             *Seats
	tier              *string
	cycle             BillingCycle
	status            SubscriptionStatus
	trialEndsAt       *time.Time
	currentPeriod     Period
	pendingChange     *PlanChange
	dunning           *DunningState
	charges           map[uuid.UUID]Charge
	coupon            *CouponApplied
	manualBilling     bool
	cancelAtPeriodEnd bool
	cancelledAt       *time.Time
	creditBalance     float64

	changes []any
}

// NewSubscription crea una instancia limpia para rehidratación.
func NewSubscription() *Subscription {
	return &Subscription{
		charges: make(map[uuid.UUID]Charge),
	}
}

// ID devuelve el identificador de la suscripción.
func (s *Subscription) ID() uuid.UUID { return s.id }

// Version devuelve la versión del agregado.
func (s *Subscription) Version() int64 { return s.version }

// AccountID devuelve la cuenta de facturación propietaria.
func (s *Subscription) AccountID() uuid.UUID { return s.accountID }

// Kind devuelve el tipo de suscripción (clínica o farmacia).
func (s *Subscription) Kind() SubscriptionKind { return s.kind }

// PlanCode devuelve el código del plan activo.
func (s *Subscription) PlanCode() string { return s.planCode }

// PlanVersion devuelve la versión del plan fijada (B10).
func (s *Subscription) PlanVersion() int { return s.planVersion }

// Seats devuelve la asignación de asientos.
func (s *Subscription) Seats() *Seats { return s.seats }

// Tier devuelve el tramo de volumen para farmacias.
func (s *Subscription) Tier() *string { return s.tier }

// Cycle devuelve la periodicidad (mensual/anual).
func (s *Subscription) Cycle() BillingCycle { return s.cycle }

// Status devuelve el estado actual (B4).
func (s *Subscription) Status() SubscriptionStatus { return s.status }

// TrialEndsAt devuelve la fecha de fin de prueba, si aplica.
func (s *Subscription) TrialEndsAt() *time.Time { return s.trialEndsAt }

// CurrentPeriod devuelve el periodo de facturación vigente.
func (s *Subscription) CurrentPeriod() Period { return s.currentPeriod }

// PendingChange devuelve el cambio diferido al fin de periodo (downgrade).
func (s *Subscription) PendingChange() *PlanChange { return s.pendingChange }

// Dunning devuelve el estado de cobros fallidos.
func (s *Subscription) Dunning() *DunningState { return s.dunning }

// Charges devuelve el histórico de cargos por ID.
func (s *Subscription) Charges() map[uuid.UUID]Charge {
	res := make(map[uuid.UUID]Charge, len(s.charges))
	for k, v := range s.charges {
		res[k] = v
	}
	return res
}

// Coupon devuelve el cupón aplicado.
func (s *Subscription) Coupon() *CouponApplied { return s.coupon }

// ManualBilling indica si es facturación manual (Enterprise).
func (s *Subscription) ManualBilling() bool { return s.manualBilling }

// CancelAtPeriodEnd indica si la cancelación está programada al término del periodo.
func (s *Subscription) CancelAtPeriodEnd() bool { return s.cancelAtPeriodEnd }

// Changes devuelve y limpia los eventos no confirmados.
func (s *Subscription) Changes() []any {
	c := s.changes
	s.changes = nil
	return c
}

func (s *Subscription) recordThat(event any) {
	s.apply(event)
	s.changes = append(s.changes, event)
}

// StartTrial inicia una prueba de 30 días para una organización (B1, RN-32).
func StartTrial(
	id uuid.UUID,
	accountID uuid.UUID,
	kind SubscriptionKind,
	plan PlanDefinition,
	now time.Time,
) (*Subscription, error) {
	if plan.TrialDays <= 0 {
		return nil, ErrTrialNotAllowed
	}

	start := now.UTC()
	end := start.AddDate(0, 0, plan.TrialDays)
	period := Period{Start: start, End: end}

	sub := NewSubscription()
	sub.recordThat(TrialStarted{
		SubscriptionID: id,
		AccountID:      accountID,
		Kind:           kind,
		PlanCode:       plan.Code,
		PlanVersion:    plan.Version,
		TrialEndsAt:    end,
		CurrentPeriod:  period,
		At:             start,
	})

	return sub, nil
}

// Subscribe contrata formalmente un plan con periodo regular (B1, B2, B3, B6, B7, B10).
func Subscribe(
	id uuid.UUID,
	accountID uuid.UUID,
	kind SubscriptionKind,
	plan PlanDefinition,
	purchasedSeats int,
	tierCode *string,
	cycle BillingCycle,
	coupon *CouponApplied,
	manualBilling bool,
	hasPaymentMethod bool,
	activeProfessionals int,
	taxRate float64,
	now time.Time,
) (*Subscription, error) {
	if !manualBilling && !hasPaymentMethod {
		return nil, ErrPaymentMethodRequired // B2
	}

	if kind == KindClinic {
		if purchasedSeats < activeProfessionals {
			return nil, ErrSeatsBelowUsage // B3
		}
		if plan.Seats != nil && purchasedSeats < plan.Seats.Min {
			return nil, fmt.Errorf("mínimo %d asientos para el plan %s", plan.Seats.Min, plan.Code)
		}
	}

	tCode := ""
	if tierCode != nil {
		tCode = *tierCode
	}

	subtotal, err := plan.CalculatePrice(purchasedSeats, tCode, cycle)
	if err != nil {
		return nil, err // B6
	}

	// Descuento por cupón si aplica
	if coupon != nil {
		switch coupon.Kind {
		case "pct":
			discount := RoundCents(subtotal * (coupon.Value / 100.0))
			subtotal = RoundCents(subtotal - discount)
		case "amount":
			subtotal = RoundCents(math.Max(0, subtotal-coupon.Value))
		}
	}

	tax := RoundCents(subtotal * taxRate)
	total := RoundCents(subtotal + tax)

	start := now.UTC()
	var end time.Time
	if cycle == CycleAnnual {
		end = start.AddDate(1, 0, 0)
	} else {
		end = start.AddDate(0, 1, 0)
	}
	period := Period{Start: start, End: end}

	sub := NewSubscription()
	var seatsObj *Seats
	if kind == KindClinic {
		seatsObj = &Seats{Purchased: purchasedSeats}
	}

	sub.recordThat(Subscribed{
		SubscriptionID: id,
		AccountID:      accountID,
		Kind:           kind,
		PlanCode:       plan.Code,
		PlanVersion:    plan.Version,
		Seats:          seatsObj,
		Tier:           tierCode,
		Cycle:          cycle,
		CurrentPeriod:  period,
		ManualBilling:  manualBilling,
		Coupon:         coupon,
		At:             start,
	})

	// B7: Genera el cargo del periodo
	chargeID := uuid.New()
	sub.recordThat(ChargeScheduled{
		ChargeID:       chargeID,
		SubscriptionID: id,
		AccountID:      accountID,
		Reason:         fmt.Sprintf("Suscripción %s (%s)", plan.Code, cycle),
		Amount:         subtotal,
		Tax:            tax,
		Total:          total,
		Currency:       plan.Pricing.Currency,
		Lines: []ChargeLine{
			{
				Description: fmt.Sprintf("Plan %s %s", plan.Code, cycle),
				Amount:      subtotal,
				Quantity:    1,
			},
		},
		Period:      &period,
		ScheduledAt: start,
	})

	return sub, nil
}

// ConvertTrialToSubscription convierte una prueba activa en suscripción definitiva.
func (s *Subscription) ConvertTrialToSubscription(
	plan PlanDefinition,
	purchasedSeats int,
	tierCode *string,
	cycle BillingCycle,
	coupon *CouponApplied,
	manualBilling bool,
	hasPaymentMethod bool,
	activeProfessionals int,
	taxRate float64,
	now time.Time,
) error {
	if s.status != StatusTrialing && s.status != StatusExpired {
		return ErrInvalidCycleTransition
	}
	if !manualBilling && !hasPaymentMethod {
		return ErrPaymentMethodRequired // B2
	}
	if s.kind == KindClinic && purchasedSeats < activeProfessionals {
		return ErrSeatsBelowUsage // B3
	}

	tCode := ""
	if tierCode != nil {
		tCode = *tierCode
	}

	subtotal, err := plan.CalculatePrice(purchasedSeats, tCode, cycle)
	if err != nil {
		return err // B6
	}

	if coupon != nil {
		switch coupon.Kind {
		case "pct":
			discount := RoundCents(subtotal * (coupon.Value / 100.0))
			subtotal = RoundCents(subtotal - discount)
		case "amount":
			subtotal = RoundCents(math.Max(0, subtotal-coupon.Value))
		}
	}

	tax := RoundCents(subtotal * taxRate)
	total := RoundCents(subtotal + tax)

	start := now.UTC()
	var end time.Time
	if cycle == CycleAnnual {
		end = start.AddDate(1, 0, 0)
	} else {
		end = start.AddDate(0, 1, 0)
	}
	period := Period{Start: start, End: end}

	var seatsObj *Seats
	if s.kind == KindClinic {
		seatsObj = &Seats{Purchased: purchasedSeats}
	}

	s.recordThat(Subscribed{
		SubscriptionID: s.id,
		AccountID:      s.accountID,
		Kind:           s.kind,
		PlanCode:       plan.Code,
		PlanVersion:    plan.Version,
		Seats:          seatsObj,
		Tier:           tierCode,
		Cycle:          cycle,
		CurrentPeriod:  period,
		ManualBilling:  manualBilling,
		Coupon:         coupon,
		At:             start,
	})

	chargeID := uuid.New()
	s.recordThat(ChargeScheduled{
		ChargeID:       chargeID,
		SubscriptionID: s.id,
		AccountID:      s.accountID,
		Reason:         fmt.Sprintf("Suscripción %s (%s)", plan.Code, cycle),
		Amount:         subtotal,
		Tax:            tax,
		Total:          total,
		Currency:       plan.Pricing.Currency,
		Lines: []ChargeLine{
			{
				Description: fmt.Sprintf("Plan %s %s", plan.Code, cycle),
				Amount:      subtotal,
				Quantity:    1,
			},
		},
		Period:      &period,
		ScheduledAt: start,
	})

	return nil
}

// ChangePlan cambia el plan o ciclo con cálculo de prorrateo (B4, B5, B6).
func (s *Subscription) ChangePlan(
	newPlan PlanDefinition,
	newSeats int,
	newTier *string,
	newCycle BillingCycle,
	currentPlan PlanDefinition,
	activeProfessionals int,
	taxRate float64,
	now time.Time,
) error {
	if s.status != StatusActive && s.status != StatusGrace {
		return ErrSubscriptionRestricted
	}
	if s.pendingChange != nil {
		return ErrPlanChangePending
	}
	if s.kind == KindClinic && newSeats < activeProfessionals {
		return ErrSeatsBelowUsage
	}

	tCode := ""
	if newTier != nil {
		tCode = *newTier
	}

	newPrice, err := newPlan.CalculatePrice(newSeats, tCode, newCycle)
	if err != nil {
		return err
	}

	currSeats := 1
	if s.seats != nil {
		currSeats = s.seats.Purchased
	}
	currTier := ""
	if s.tier != nil {
		currTier = *s.tier
	}
	oldPrice, err := currentPlan.CalculatePrice(currSeats, currTier, s.cycle)
	if err != nil {
		return err
	}

	// Upgrade si el nuevo precio periódico es superior o cambio a ciclo anual
	isUpgrade := newPrice > oldPrice || (newCycle == CycleAnnual && s.cycle == CycleMonthly)

	if isUpgrade {
		// B5: Prorrateo = (días restantes / días del periodo) * (nuevoPrecio - precioAntiguo)
		totalDays := s.currentPeriod.TotalDays()
		remDays := s.currentPeriod.RemainingDays(now)
		var prorationAmount float64
		if totalDays > 0 && remDays > 0 {
			fraction := float64(remDays) / float64(totalDays)
			prorationAmount = RoundCents(fraction * (newPrice - oldPrice))
			if prorationAmount < 0 {
				prorationAmount = 0
			}
		}

		var seatsObj *Seats
		if s.kind == KindClinic {
			seatsObj = &Seats{Purchased: newSeats}
		}

		s.recordThat(PlanChangeApplied{
			SubscriptionID: s.id,
			PlanCode:       newPlan.Code,
			PlanVersion:    newPlan.Version,
			Seats:          seatsObj,
			Tier:           newTier,
			Cycle:          newCycle,
			At:             now.UTC(),
		})

		if prorationAmount > 0 {
			tax := RoundCents(prorationAmount * taxRate)
			total := RoundCents(prorationAmount + tax)
			s.recordThat(ChargeScheduled{
				ChargeID:       uuid.New(),
				SubscriptionID: s.id,
				AccountID:      s.accountID,
				Reason:         fmt.Sprintf("Prorrateo upgrade a %s (%d días restantes)", newPlan.Code, remDays),
				Amount:         prorationAmount,
				Tax:            tax,
				Total:          total,
				Currency:       newPlan.Pricing.Currency,
				Lines: []ChargeLine{
					{
						Description: fmt.Sprintf("Prorrateo %s (%d/%d días)", newPlan.Code, remDays, totalDays),
						Amount:      prorationAmount,
						Quantity:    1,
					},
				},
				Period:      &s.currentPeriod,
				ScheduledAt: now.UTC(),
			})
		}
	} else {
		// Downgrade: programado al término del periodo actual (B3, B9)
		var sPtr *int
		if s.kind == KindClinic {
			sPtr = &newSeats
		}
		s.recordThat(PlanChangeScheduled{
			SubscriptionID: s.id,
			PendingChange: PlanChange{
				PlanCode:    newPlan.Code,
				PlanVersion: newPlan.Version,
				Seats:       sPtr,
				Tier:        newTier,
				Cycle:       newCycle,
				ScheduledAt: s.currentPeriod.End,
			},
			At: now.UTC(),
		})
	}

	return nil
}

// AdjustSeats modifica los asientos contratados para clínicas (B3, B5, B7).
func (s *Subscription) AdjustSeats(
	newSeats int,
	plan PlanDefinition,
	activeProfessionals int,
	taxRate float64,
	now time.Time,
) error {
	if s.status != StatusActive && s.status != StatusGrace {
		return ErrSubscriptionRestricted
	}
	if s.kind != KindClinic {
		return fmt.Errorf("los asientos solo aplican a planes clínicos")
	}
	if newSeats < activeProfessionals {
		return ErrSeatsBelowUsage // B3
	}
	if plan.Seats != nil {
		if newSeats < plan.Seats.Min {
			return fmt.Errorf("mínimo %d asientos requeridos", plan.Seats.Min)
		}
		if plan.Seats.Max != nil && newSeats > *plan.Seats.Max {
			return fmt.Errorf("máximo %d asientos permitidos", *plan.Seats.Max)
		}
	}

	currPurchased := 0
	if s.seats != nil {
		currPurchased = s.seats.Purchased
	}
	delta := newSeats - currPurchased
	if delta == 0 {
		return nil
	}

	s.recordThat(SeatsAdjusted{
		SubscriptionID: s.id,
		Seats:          Seats{Purchased: newSeats},
		Delta:          delta,
		At:             now.UTC(),
	})

	// Si aumenta asientos durante el ciclo activo, genera prorrateo inmediato (B5, B7)
	if delta > 0 && plan.Pricing.Model == "per_seat" {
		unitPrice := plan.Pricing.Monthly
		if s.cycle == CycleAnnual {
			unitPrice = plan.Pricing.Annual
		}
		additionalFullCost := float64(delta) * unitPrice

		totalDays := s.currentPeriod.TotalDays()
		remDays := s.currentPeriod.RemainingDays(now)
		if totalDays > 0 && remDays > 0 {
			fraction := float64(remDays) / float64(totalDays)
			prorated := RoundCents(fraction * additionalFullCost)
			if prorated > 0 {
				tax := RoundCents(prorated * taxRate)
				total := RoundCents(prorated + tax)
				s.recordThat(ChargeScheduled{
					ChargeID:       uuid.New(),
					SubscriptionID: s.id,
					AccountID:      s.accountID,
					Reason:         fmt.Sprintf("Prorrateo de %d asiento(s) adicional(es)", delta),
					Amount:         prorated,
					Tax:            tax,
					Total:          total,
					Currency:       plan.Pricing.Currency,
					Lines: []ChargeLine{
						{
							Description: fmt.Sprintf("Asiento(s) extra x%d (%d/%d días)", delta, remDays, totalDays),
							Amount:      prorated,
							Quantity:    delta,
						},
					},
					Period:      &s.currentPeriod,
					ScheduledAt: now.UTC(),
				})
			}
		}
	}

	return nil
}

// RenewPeriod renueva el periodo y aplica cambios pendientes (B7, B10).
func (s *Subscription) RenewPeriod(
	planCatalog PlanDefinition,
	activeProfessionals int,
	taxRate float64,
	now time.Time,
) error {
	if s.status != StatusActive && s.status != StatusGrace && s.status != StatusRestricted {
		return ErrInvalidCycleTransition
	}

	// Si estaba cancelada al fin de periodo, expira definitivamente (B9)
	if s.cancelAtPeriodEnd {
		s.recordThat(SubscriptionCancelled{
			SubscriptionID: s.id,
			Reason:         "Periodo concluido tras cancelación programada",
			At:             now.UTC(),
		})
		return nil
	}

	planCode := s.planCode
	planVer := s.planVersion
	cycle := s.cycle
	seats := 1
	if s.seats != nil {
		seats = s.seats.Purchased
	}
	tier := ""
	if s.tier != nil {
		tier = *s.tier
	}

	// Si hay un cambio diferido pendiente, se aplica al renovar
	if s.pendingChange != nil {
		if s.pendingChange.Seats != nil && *s.pendingChange.Seats < activeProfessionals {
			// No se puede aplicar si supera profesionales actuales (B3)
			return ErrSeatsBelowUsage
		}
		planCode = s.pendingChange.PlanCode
		planVer = s.pendingChange.PlanVersion
		cycle = s.pendingChange.Cycle
		if s.pendingChange.Seats != nil {
			seats = *s.pendingChange.Seats
		}
		if s.pendingChange.Tier != nil {
			tier = *s.pendingChange.Tier
		}
	} else {
		// B10 / RN-39: si el catálogo tiene una nueva versión publicada con >= 30 días de antelación
		if planCatalog.Version > s.planVersion && planCatalog.PublishedAt != nil {
			publishedAgo := now.Sub(*planCatalog.PublishedAt).Hours() / 24
			if publishedAgo >= 30 {
				planVer = planCatalog.Version
			}
		}
	}

	subtotal, err := planCatalog.CalculatePrice(seats, tier, cycle)
	if err != nil {
		return err
	}
	lines := []ChargeLine{{
		Description: fmt.Sprintf("Renovación %s (%s)", planCode, cycle),
		Amount:      subtotal,
		Quantity:    1,
	}}
	// Cupón vigente (PRD-022) y crédito de cortesía (PRD-011) reducen la base gravable.
	if d := s.coupon.discount(subtotal, s.currentPeriod.End); d > 0 {
		lines = append(lines, ChargeLine{Description: "Cupón " + s.coupon.Code, Amount: -d, Quantity: 1})
		subtotal = RoundCents(subtotal - d)
	}
	credit := RoundCents(math.Min(s.creditBalance, subtotal))
	if credit > 0 {
		lines = append(lines, ChargeLine{Description: "Crédito de cortesía", Amount: -credit, Quantity: 1})
		subtotal = RoundCents(subtotal - credit)
	}

	tax := RoundCents(subtotal * taxRate)
	total := RoundCents(subtotal + tax)

	start := s.currentPeriod.End
	var end time.Time
	if cycle == CycleAnnual {
		end = start.AddDate(1, 0, 0)
	} else {
		end = start.AddDate(0, 1, 0)
	}
	newPeriod := Period{Start: start, End: end}

	s.recordThat(PeriodRenewed{
		SubscriptionID: s.id,
		PlanCode:       planCode,
		PlanVersion:    planVer,
		CurrentPeriod:  newPeriod,
		At:             now.UTC(),
	})

	// B7: Programa el cargo del nuevo periodo
	s.recordThat(ChargeScheduled{
		ChargeID:       uuid.New(),
		SubscriptionID: s.id,
		AccountID:      s.accountID,
		Reason:         fmt.Sprintf("Renovación %s (%s)", planCode, cycle),
		Amount:         subtotal,
		Tax:            tax,
		Total:          total,
		Currency:       planCatalog.Pricing.Currency,
		Lines:          lines,
		Period:         &newPeriod,
		ScheduledAt:    start,
		CreditApplied:  credit,
	})

	return nil
}

// RecordChargeResult procesa la confirmación o rechazo de un intento de cobro (B4).
// Conserva la firma de M5.1; delega en RecordChargePaid / RecordChargeFailed.
func (s *Subscription) RecordChargeResult(
	chargeID uuid.UUID,
	result string, // "paid" | "failed"
	paymentRef string,
	errorCode string,
	now time.Time,
) error {
	if result == "paid" {
		return s.RecordChargePaid(chargeID, "", paymentRef, now)
	}
	return s.RecordChargeFailed(chargeID, errorCode, now)
}

// EnterGrace marca el inicio de gracia por dunning (ADR-025).
func (s *Subscription) EnterGrace(graceEndsAt time.Time, now time.Time) error {
	if s.status != StatusActive {
		return nil
	}
	s.recordThat(SubscriptionGraceStarted{
		SubscriptionID: s.id,
		GraceEndsAt:    graceEndsAt.UTC(),
		At:             now.UTC(),
	})
	return nil
}

// Restrict restringe la suscripción tras agotar periodo de gracia (B4).
func (s *Subscription) Restrict(reason string, now time.Time) error {
	if s.status == StatusRestricted || s.status == StatusCancelled {
		return nil
	}
	s.recordThat(SubscriptionRestricted{
		SubscriptionID: s.id,
		Reason:         reason,
		At:             now.UTC(),
	})
	return nil
}

// Reactivate reactiva la suscripción completa (B4).
func (s *Subscription) Reactivate(now time.Time) error {
	if s.status == StatusActive {
		return nil
	}
	s.recordThat(SubscriptionReactivated{
		SubscriptionID: s.id,
		At:             now.UTC(),
	})
	return nil
}

// Cancel cancela la suscripción inmediatamente o al término del periodo (B9).
func (s *Subscription) Cancel(atPeriodEnd bool, reason string, now time.Time) error {
	if s.status == StatusCancelled {
		return nil
	}

	if atPeriodEnd {
		s.recordThat(SubscriptionCancellationScheduled{
			SubscriptionID: s.id,
			EffectiveAt:    s.currentPeriod.End,
			Reason:         reason,
			At:             now.UTC(),
		})
	} else {
		s.recordThat(SubscriptionCancelled{
			SubscriptionID: s.id,
			Reason:         reason,
			At:             now.UTC(),
		})
	}

	return nil
}

// Resume revierte una cancelación programada (B9).
func (s *Subscription) Resume(now time.Time) error {
	if !s.cancelAtPeriodEnd {
		return nil
	}
	s.recordThat(SubscriptionResumed{
		SubscriptionID: s.id,
		At:             now.UTC(),
	})
	return nil
}

// ExpireTrial marca el fin del periodo de prueba sin conversión (PP-F5-PRD-002).
func (s *Subscription) ExpireTrial(now time.Time) error {
	if s.status != StatusTrialing {
		return nil
	}
	// 7 días de gracia tras vencimiento de prueba
	graceEnd := now.AddDate(0, 0, 7).UTC()
	s.recordThat(TrialExpired{
		SubscriptionID: s.id,
		GraceEndsAt:    graceEnd,
		At:             now.UTC(),
	})
	return nil
}

// ApplyAdminAdjustment aplica modificaciones manuales del SuperAdmin.
func (s *Subscription) ApplyAdminAdjustment(kind string, details string, reason string, now time.Time) error {
	s.recordThat(AdminAdjustmentApplied{
		SubscriptionID: s.id,
		Kind:           kind,
		Details:        details,
		Reason:         reason,
		At:             now.UTC(),
	})
	return nil
}

// RecordManualPayment asienta un pago recibido fuera del proveedor (transferencia):
// planes Enterprise (PRD-009) o regularización por SuperAdmin (PRD-011). El importe
// debe coincidir con el total del cargo para conservar Σ cargos − reembolsos = Σ pagos.
func (s *Subscription) RecordManualPayment(chargeID uuid.UUID, amount float64, ref string, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusPending && ch.Status != ChargeStatusFailed {
		return ErrChargeNotPending
	}
	if RoundCents(amount) != ch.Total {
		return ErrManualAmountMismatch
	}
	s.recordPaid(ch, "manual", ref, true, now)
	return nil
}

// Refund procesa un reembolso parcial o total de un cargo pagado (B8).
func (s *Subscription) Refund(chargeID uuid.UUID, amount float64, reason string, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("cargo %s no encontrado", chargeID)
	}
	if ch.Status != ChargeStatusPaid && ch.Status != ChargeStatusPartiallyRefunded && ch.Status != ChargeStatusRefunded {
		return ErrChargeNotPaid
	}

	refundable := RoundCents(ch.Total - ch.Refunded)
	if amount > refundable || amount <= 0 {
		return ErrRefundExceeds // B8
	}

	s.recordThat(ChargeRefunded{
		SubscriptionID: s.id,
		ChargeID:       chargeID,
		Amount:         RoundCents(amount),
		Reason:         reason,
		RefundedAt:     now.UTC(),
	})
	return nil
}

// Apply rehidrata el estado del agregado desde eventos.
func (s *Subscription) Apply(event any) {
	s.apply(event)
}

func (s *Subscription) apply(event any) {
	switch e := event.(type) {
	case TrialStarted:
		s.id = e.SubscriptionID
		s.accountID = e.AccountID
		s.kind = e.Kind
		s.planCode = e.PlanCode
		s.planVersion = e.PlanVersion
		s.trialEndsAt = &e.TrialEndsAt
		s.currentPeriod = e.CurrentPeriod
		s.status = StatusTrialing
	case Subscribed:
		s.id = e.SubscriptionID
		s.accountID = e.AccountID
		s.kind = e.Kind
		s.planCode = e.PlanCode
		s.planVersion = e.PlanVersion
		s.seats = e.Seats
		s.tier = e.Tier
		s.cycle = e.Cycle
		s.currentPeriod = e.CurrentPeriod
		s.manualBilling = e.ManualBilling
		s.coupon = e.Coupon
		s.status = StatusActive
		s.cancelAtPeriodEnd = false
		s.pendingChange = nil
	case PlanChangeApplied:
		s.planCode = e.PlanCode
		s.planVersion = e.PlanVersion
		if e.Seats != nil {
			s.seats = e.Seats
		}
		if e.Tier != nil {
			s.tier = e.Tier
		}
		s.cycle = e.Cycle
		s.pendingChange = nil
	case PlanChangeScheduled:
		pc := e.PendingChange
		s.pendingChange = &pc
	case SeatsAdjusted:
		s.seats = &e.Seats
	case PeriodRenewed:
		s.planCode = e.PlanCode
		s.planVersion = e.PlanVersion
		s.currentPeriod = e.CurrentPeriod
		s.pendingChange = nil
	case ChargeScheduled:
		ch := Charge{
			ID:             e.ChargeID,
			SubscriptionID: e.SubscriptionID,
			AccountID:      e.AccountID,
			Reason:         e.Reason,
			Amount:         e.Amount,
			Tax:            e.Tax,
			Total:          e.Total,
			Currency:       e.Currency,
			Lines:          e.Lines,
			ScheduledAt:    e.ScheduledAt,
			Status:         ChargeStatusPending,
		}
		if e.Period != nil {
			ch.PeriodStart = &e.Period.Start
			ch.PeriodEnd = &e.Period.End
		}
		s.charges[e.ChargeID] = ch
		s.creditBalance = RoundCents(s.creditBalance - e.CreditApplied)
	case ChargePaid:
		ch := s.charges[e.ChargeID]
		ch.Status = ChargeStatusPaid
		paidAt := e.PaidAt
		ch.PaidAt = &paidAt
		pref := e.PaymentRef
		ch.PaymentRef = &pref
		s.charges[e.ChargeID] = ch
		if !s.hasFailedCharges() {
			s.dunning = nil
		}
	case ChargeFailed:
		ch := s.charges[e.ChargeID]
		ch.Status = ChargeStatusFailed
		failedAt := e.FailedAt
		ch.FailedAt = &failedAt
		ec := e.ErrorCode
		ch.ErrorCode = &ec
		s.charges[e.ChargeID] = ch
		if s.dunning == nil {
			s.dunning = &DunningState{FailedAt: e.FailedAt, Attempts: 1}
		} else {
			s.dunning.Attempts++
		}
	case ChargeRefunded:
		ch := s.charges[e.ChargeID]
		ch.Refunded = RoundCents(ch.Refunded + e.Amount)
		if ch.Refunded >= ch.Total {
			ch.Status = ChargeStatusRefunded
		} else {
			ch.Status = ChargeStatusPartiallyRefunded
		}
		s.charges[e.ChargeID] = ch
	case SubscriptionGraceStarted:
		s.status = StatusGrace
		if s.dunning != nil {
			ge := e.GraceEndsAt
			s.dunning.GraceEndsAt = &ge
		}
	case SubscriptionRestricted:
		s.status = StatusRestricted
	case SubscriptionReactivated:
		s.status = StatusActive
		s.dunning = nil
	case SubscriptionCancellationScheduled:
		s.cancelAtPeriodEnd = true
	case SubscriptionCancelled:
		s.status = StatusCancelled
		s.cancelAtPeriodEnd = false
		at := e.At
		s.cancelledAt = &at
	case SubscriptionResumed:
		s.cancelAtPeriodEnd = false
	case TrialExpired:
		s.status = StatusRestricted
	case AdminAdjustmentApplied:
		if e.TrialEndsAt != nil {
			end := *e.TrialEndsAt
			s.trialEndsAt = &end
			s.currentPeriod.End = end
			s.status = StatusTrialing
		}
		s.creditBalance = RoundCents(s.creditBalance + e.Credit)
	case CouponAppliedEvent:
		c := e.Coupon
		s.coupon = &c
	}
	s.version++
}
