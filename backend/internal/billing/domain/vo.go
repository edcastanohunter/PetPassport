package domain

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// Money representa una cantidad monetaria con precisión de 2 decimales (PP-F5-DOM-005).
type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// NewMoney crea un valor monetario redondeado a 2 decimales.
func NewMoney(amount float64, currency string) Money {
	return Money{
		Amount:   RoundCents(amount),
		Currency: currency,
	}
}

// RoundCents redondea un importe a 2 decimales según reglas bancarias estándar.
func RoundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// Add suma dos importes de la misma moneda.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != "" && o.Currency != "" && m.Currency != o.Currency {
		return Money{}, fmt.Errorf("monedas incompatibles: %s != %s", m.Currency, o.Currency)
	}
	cur := m.Currency
	if cur == "" {
		cur = o.Currency
	}
	return NewMoney(m.Amount+o.Amount, cur), nil
}

// Sub resta dos importes de la misma moneda.
func (m Money) Sub(o Money) (Money, error) {
	if m.Currency != "" && o.Currency != "" && m.Currency != o.Currency {
		return Money{}, fmt.Errorf("monedas incompatibles: %s != %s", m.Currency, o.Currency)
	}
	cur := m.Currency
	if cur == "" {
		cur = o.Currency
	}
	return NewMoney(m.Amount-o.Amount, cur), nil
}

// BillingCycle define la periodicidad de cobro.
type BillingCycle string

const (
	CycleMonthly BillingCycle = "monthly"
	CycleAnnual  BillingCycle = "annual"
)

// SubscriptionKind indica si la suscripción aplica a clínica o farmacia.
type SubscriptionKind string

const (
	KindClinic   SubscriptionKind = "clinic"
	KindPharmacy SubscriptionKind = "pharmacy"
)

// SubscriptionStatus define el ciclo de vida de la suscripción (DOM-004, B4).
type SubscriptionStatus string

const (
	StatusTrialing   SubscriptionStatus = "trialing"
	StatusActive     SubscriptionStatus = "active"
	StatusGrace      SubscriptionStatus = "grace"
	StatusRestricted SubscriptionStatus = "restricted"
	StatusCancelled  SubscriptionStatus = "cancelled"
	StatusExpired    SubscriptionStatus = "expired"
)

// Period representa un intervalo temporal de facturación.
type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// TotalDays devuelve la cantidad de días del periodo (mínimo 1).
func (p Period) TotalDays() int {
	days := int(p.End.Sub(p.Start).Hours() / 24)
	if days <= 0 {
		return 1
	}
	return days
}

// RemainingDays calcula los días restantes desde now hasta End (0 si ya pasó).
func (p Period) RemainingDays(now time.Time) int {
	if now.After(p.End) || now.Equal(p.End) {
		return 0
	}
	if now.Before(p.Start) {
		return p.TotalDays()
	}
	hours := p.End.Sub(now).Hours()
	days := int(math.Ceil(hours / 24.0))
	if days < 0 {
		return 0
	}
	return days
}

// Seats define la cantidad de asientos profesionales contratados.
type Seats struct {
	Purchased int `json:"purchased"`
}

// VolumeTier define los tramos de volumen para farmacias (DOM-005).
type VolumeTier struct {
	Code  string  `json:"code"`
	UpTo  int     `json:"up_to"` // -1 = ilimitado
	Price float64 `json:"price"`
}

// CouponApplied almacena información de descuentos aplicados (DOM-005).
type CouponApplied struct {
	Code  string     `json:"code"`
	Kind  string     `json:"kind"` // "pct" | "amount"
	Value float64    `json:"value"`
	Until *time.Time `json:"until,omitempty"`
}

// TaxID identifica fiscalmente a una persona o empresa.
type TaxID struct {
	Country string `json:"country"`
	Value   string `json:"value"`
	Kind    string `json:"kind"` // NIT, CC, etc.
}

// LegalEntity contiene datos fiscales de facturación (DOM-003).
type LegalEntity struct {
	Kind      string  `json:"kind"` // "company" | "person"
	LegalName string  `json:"legal_name"`
	TaxID     TaxID   `json:"tax_id"`
	Address   string  `json:"address"`
	Email     string  `json:"email"`
	Phone     *string `json:"phone,omitempty"`
	TaxRegime *string `json:"tax_regime,omitempty"`
}

// PaymentMethodRef hace referencia al método tokenizado en el proveedor (DOM-003, SEC-001).
type PaymentMethodRef struct {
	Provider  string     `json:"provider"`
	Token     string     `json:"token"`
	Kind      string     `json:"kind"` // "card" | "pse" | "manual"
	Last4     *string    `json:"last4,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Reusable indica si el proveedor guarda un token con el que cobrar sin el
// pagador. PSE y el pago manual no lo tienen: cada cargo lo paga el cliente.
func (r PaymentMethodRef) Reusable() bool { return r.Kind != "pse" && r.Kind != "manual" }

// PaymentProviderRef asocia la cuenta al cliente en la pasarela.
type PaymentProviderRef struct {
	Name       string `json:"name"`
	CustomerID string `json:"customer_id"`
}

// AccountOrgMembership define la membresía de una organización en una cuenta de facturación.
type AccountOrgMembership struct {
	JoinedAt time.Time `json:"joined_at"`
	SeatPool string    `json:"seat_pool"` // "own" | "shared"
}

// ChargeStatus representa las etapas de cobro de un cargo.
type ChargeStatus string

const (
	ChargeStatusPending           ChargeStatus = "pending"
	ChargeStatusPaid              ChargeStatus = "paid"
	ChargeStatusFailed            ChargeStatus = "failed"
	ChargeStatusRefunded          ChargeStatus = "refunded"
	ChargeStatusPartiallyRefunded ChargeStatus = "partially_refunded"
	ChargeStatusVoid              ChargeStatus = "void"
)

// ChargeLine detalla un concepto dentro del cargo.
type ChargeLine struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Quantity    int     `json:"quantity"`
}

// Charge representa un cargo programado o ejecutado (DOM-004, B7).
type Charge struct {
	ID             uuid.UUID    `json:"id"`
	SubscriptionID uuid.UUID    `json:"subscription_id"`
	AccountID      uuid.UUID    `json:"account_id"`
	Reason         string       `json:"reason"`
	PeriodStart    *time.Time   `json:"period_start,omitempty"`
	PeriodEnd      *time.Time   `json:"period_end,omitempty"`
	Amount         float64      `json:"amount"`
	Tax            float64      `json:"tax"`
	Total          float64      `json:"total"`
	Currency       string       `json:"currency"`
	Status         ChargeStatus `json:"status"`
	Refunded       float64      `json:"refunded"`
	Lines          []ChargeLine `json:"lines"`
	ScheduledAt    time.Time    `json:"scheduled_at"`
	PaidAt         *time.Time   `json:"paid_at,omitempty"`
	FailedAt       *time.Time   `json:"failed_at,omitempty"`
	ErrorCode      *string      `json:"error_code,omitempty"`
	PaymentRef     *string      `json:"payment_ref,omitempty"`
}

// PlanChange describe una modificación diferida de suscripción (downgrades).
type PlanChange struct {
	PlanCode    string       `json:"plan_code"`
	PlanVersion int          `json:"plan_version"`
	Seats       *int         `json:"seats,omitempty"`
	Tier        *string      `json:"tier,omitempty"`
	Cycle       BillingCycle `json:"cycle"`
	ScheduledAt time.Time    `json:"scheduled_at"`
}

// DunningState rastrea los reintentos tras fallo de cobro (DOM-004, ARQ-006).
type DunningState struct {
	FailedAt      time.Time  `json:"failed_at"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	GraceEndsAt   *time.Time `json:"grace_ends_at,omitempty"`
}

// PlanPricing contiene las opciones de precio de un plan.
type PlanPricing struct {
	Model    string       `json:"model"` // "flat" | "per_seat" | "tiered"
	Currency string       `json:"currency"`
	Monthly  float64      `json:"monthly"`
	Annual   float64      `json:"annual"`
	Tiers    []VolumeTier `json:"tiers,omitempty"`
}

// PlanSeatsRange define los límites de asientos configurables.
type PlanSeatsRange struct {
	Min int  `json:"min"`
	Max *int `json:"max,omitempty"`
}

// PlanDefinition es la estructura del catálogo versionado de planes (DOM-005, DAT-002).
type PlanDefinition struct {
	Code          string            `json:"code"`
	Version       int               `json:"version"`
	Kind          string            `json:"kind"` // "clinic" | "pharmacy"
	Name          map[string]string `json:"name"` // "es", "en"
	Pricing       PlanPricing       `json:"pricing"`
	TrialDays     int               `json:"trial_days"`
	Seats         *PlanSeatsRange   `json:"seats,omitempty"`
	Quotas        map[string]int64  `json:"quotas"`
	Features      []string          `json:"features"`
	Status        string            `json:"status"` // "draft" | "published" | "retired"
	EffectiveFrom *time.Time        `json:"effective_from,omitempty"`
	PublishedAt   *time.Time        `json:"published_at,omitempty"`
}

// CalculatePrice calcula el subtotal para un plan según asientos o tier y ciclo.
func (p PlanDefinition) CalculatePrice(seats int, tierCode string, cycle BillingCycle) (float64, error) {
	var base float64
	switch p.Pricing.Model {
	case "flat":
		if cycle == CycleAnnual {
			base = p.Pricing.Annual
		} else {
			base = p.Pricing.Monthly
		}
	case "per_seat":
		if seats <= 0 {
			seats = 1
		}
		if p.Seats != nil {
			if seats < p.Seats.Min {
				return 0, fmt.Errorf("mínimo %d asientos requeridos", p.Seats.Min)
			}
			if p.Seats.Max != nil && seats > *p.Seats.Max {
				return 0, fmt.Errorf("máximo %d asientos permitidos", *p.Seats.Max)
			}
		}
		unit := p.Pricing.Monthly
		if cycle == CycleAnnual {
			unit = p.Pricing.Annual
		}
		base = unit * float64(seats)
	case "tiered":
		var found bool
		for _, t := range p.Pricing.Tiers {
			if t.Code == tierCode {
				base = t.Price
				if cycle == CycleAnnual {
					base = t.Price * 10 // Descuento anual: 10 meses
				}
				found = true
				break
			}
		}
		if !found {
			// Por defecto primer tier si no se especifica
			if len(p.Pricing.Tiers) > 0 {
				base = p.Pricing.Tiers[0].Price
				if cycle == CycleAnnual {
					base = base * 10
				}
			} else {
				if cycle == CycleAnnual {
					base = p.Pricing.Annual
				} else {
					base = p.Pricing.Monthly
				}
			}
		}
	default:
		return 0, fmt.Errorf("modelo de precio desconocido: %s", p.Pricing.Model)
	}
	return RoundCents(base), nil
}
