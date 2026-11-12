package domain

import (
	"time"

	"github.com/google/uuid"
)

// BillingAccountOpened se emite al crear una nueva cuenta de facturación (PP-F5-DOM-006).
type BillingAccountOpened struct {
	AccountID   uuid.UUID   `json:"account_id"`
	OwnerUserID uuid.UUID   `json:"owner_user_id"`
	LegalEntity LegalEntity `json:"legal_entity"`
	Country     string      `json:"country"`
	Currency    string      `json:"currency"`
	At          time.Time   `json:"at"`
}

func (BillingAccountOpened) EventName() string  { return "BillingAccountOpened" }
func (BillingAccountOpened) SchemaVersion() int { return 1 }

// LegalEntityUpdated registra la actualización de datos fiscales de la cuenta.
type LegalEntityUpdated struct {
	AccountID   uuid.UUID   `json:"account_id"`
	LegalEntity LegalEntity `json:"legal_entity"`
	At          time.Time   `json:"at"`
}

func (LegalEntityUpdated) EventName() string  { return "LegalEntityUpdated" }
func (LegalEntityUpdated) SchemaVersion() int { return 1 }

// OrganizationAttached vincula una organización a la cuenta de facturación.
type OrganizationAttached struct {
	AccountID      uuid.UUID `json:"account_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	SeatPool       string    `json:"seat_pool"`
	At             time.Time `json:"at"`
}

func (OrganizationAttached) EventName() string  { return "OrganizationAttached" }
func (OrganizationAttached) SchemaVersion() int { return 1 }

// OrganizationDetached desvincula una organización de la cuenta.
type OrganizationDetached struct {
	AccountID      uuid.UUID `json:"account_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	At             time.Time `json:"at"`
}

func (OrganizationDetached) EventName() string  { return "OrganizationDetached" }
func (OrganizationDetached) SchemaVersion() int { return 1 }

// PaymentMethodSet actualiza el método de pago por defecto de la cuenta.
type PaymentMethodSet struct {
	AccountID     uuid.UUID        `json:"account_id"`
	PaymentMethod PaymentMethodRef `json:"payment_method"`
	At            time.Time        `json:"at"`
}

func (PaymentMethodSet) EventName() string  { return "PaymentMethodSet" }
func (PaymentMethodSet) SchemaVersion() int { return 1 }

// BillingAccountClosed cierra definitivamente la cuenta de facturación.
type BillingAccountClosed struct {
	AccountID uuid.UUID `json:"account_id"`
	At        time.Time `json:"at"`
}

func (BillingAccountClosed) EventName() string  { return "BillingAccountClosed" }
func (BillingAccountClosed) SchemaVersion() int { return 1 }

// TrialStarted inicia el periodo de prueba de un plan (PP-F5-DOM-006).
type TrialStarted struct {
	SubscriptionID uuid.UUID        `json:"subscription_id"`
	AccountID      uuid.UUID        `json:"account_id"`
	Kind           SubscriptionKind `json:"kind"`
	PlanCode       string           `json:"plan_code"`
	PlanVersion    int              `json:"plan_version"`
	TrialEndsAt    time.Time        `json:"trial_ends_at"`
	CurrentPeriod  Period           `json:"current_period"`
	At             time.Time        `json:"at"`
}

func (TrialStarted) EventName() string  { return "TrialStarted" }
func (TrialStarted) SchemaVersion() int { return 1 }

// Subscribed activa una suscripción de pago para la cuenta.
type Subscribed struct {
	SubscriptionID uuid.UUID        `json:"subscription_id"`
	AccountID      uuid.UUID        `json:"account_id"`
	Kind           SubscriptionKind `json:"kind"`
	PlanCode       string           `json:"plan_code"`
	PlanVersion    int              `json:"plan_version"`
	Seats          *Seats           `json:"seats,omitempty"`
	Tier           *string          `json:"tier,omitempty"`
	Cycle          BillingCycle     `json:"cycle"`
	CurrentPeriod  Period           `json:"current_period"`
	ManualBilling  bool             `json:"manual_billing"`
	Coupon         *CouponApplied   `json:"coupon,omitempty"`
	At             time.Time        `json:"at"`
}

func (Subscribed) EventName() string  { return "Subscribed" }
func (Subscribed) SchemaVersion() int { return 1 }

// PlanChangeApplied aplica inmediatamente un cambio de plan (upgrade).
type PlanChangeApplied struct {
	SubscriptionID uuid.UUID    `json:"subscription_id"`
	PlanCode       string       `json:"plan_code"`
	PlanVersion    int          `json:"plan_version"`
	Seats          *Seats       `json:"seats,omitempty"`
	Tier           *string      `json:"tier,omitempty"`
	Cycle          BillingCycle `json:"cycle"`
	At             time.Time    `json:"at"`
}

func (PlanChangeApplied) EventName() string  { return "PlanChangeApplied" }
func (PlanChangeApplied) SchemaVersion() int { return 1 }

// PlanChangeScheduled programa un cambio de plan al término del periodo actual (downgrade).
type PlanChangeScheduled struct {
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	PendingChange  PlanChange `json:"pending_change"`
	At             time.Time  `json:"at"`
}

func (PlanChangeScheduled) EventName() string  { return "PlanChangeScheduled" }
func (PlanChangeScheduled) SchemaVersion() int { return 1 }

// SeatsAdjusted modifica los asientos profesionales de la suscripción.
type SeatsAdjusted struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	Seats          Seats     `json:"seats"`
	Delta          int       `json:"delta"`
	At             time.Time `json:"at"`
}

func (SeatsAdjusted) EventName() string  { return "SeatsAdjusted" }
func (SeatsAdjusted) SchemaVersion() int { return 1 }

// PeriodRenewed renueva el periodo de suscripción y aplica cambios diferidos si existen.
type PeriodRenewed struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	PlanCode       string    `json:"plan_code"`
	PlanVersion    int       `json:"plan_version"`
	CurrentPeriod  Period    `json:"current_period"`
	At             time.Time `json:"at"`
}

func (PeriodRenewed) EventName() string  { return "PeriodRenewed" }
func (PeriodRenewed) SchemaVersion() int { return 1 }

// ChargeScheduled programa un cargo a cobrar por la suscripción (B7).
type ChargeScheduled struct {
	ChargeID       uuid.UUID    `json:"charge_id"`
	SubscriptionID uuid.UUID    `json:"subscription_id"`
	AccountID      uuid.UUID    `json:"account_id"`
	Reason         string       `json:"reason"`
	Amount         float64      `json:"amount"`
	Tax            float64      `json:"tax"`
	Total          float64      `json:"total"`
	Currency       string       `json:"currency"`
	Lines          []ChargeLine `json:"lines"`
	Period         *Period      `json:"period,omitempty"`
	ScheduledAt    time.Time    `json:"scheduled_at"`
	// CreditApplied es el crédito de cortesía consumido por este cargo (aditivo, M5.2).
	CreditApplied float64 `json:"credit_applied,omitempty"`
}

func (ChargeScheduled) EventName() string  { return "ChargeScheduled" }
func (ChargeScheduled) SchemaVersion() int { return 1 }

// ChargePaid documenta la confirmación de pago de un cargo.
type ChargePaid struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	ChargeID       uuid.UUID `json:"charge_id"`
	Amount         float64   `json:"amount"`
	PaymentRef     string    `json:"payment_ref"`
	Manual         bool      `json:"manual"`
	PaidAt         time.Time `json:"paid_at"`
	// Provider es el adaptador que confirmó el pago (aditivo, M5.2).
	Provider string `json:"provider,omitempty"`
}

func (ChargePaid) EventName() string  { return "ChargePaid" }
func (ChargePaid) SchemaVersion() int { return 1 }

// ChargeFailed registra el fallo en el cobro de un cargo.
type ChargeFailed struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	ChargeID       uuid.UUID `json:"charge_id"`
	ErrorCode      string    `json:"error_code"`
	FailedAt       time.Time `json:"failed_at"`
}

func (ChargeFailed) EventName() string  { return "ChargeFailed" }
func (ChargeFailed) SchemaVersion() int { return 1 }

// ChargeRefunded registra un reembolso parcial o total de un cargo pagado.
type ChargeRefunded struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	ChargeID       uuid.UUID `json:"charge_id"`
	Amount         float64   `json:"amount"`
	Reason         string    `json:"reason"`
	RefundedAt     time.Time `json:"refunded_at"`
	// RefundRef y Provider identifican el reembolso en el proveedor (aditivos, M5.2).
	RefundRef string `json:"refund_ref,omitempty"`
	Provider  string `json:"provider,omitempty"`
	// RefundedTotal es el total reembolsado del cargo tras este reembolso
	// (aditivo, M5.2). Hace idempotente la proyección: reaplicar el evento no
	// vuelve a sumar Amount. Cero en eventos anteriores a M5.2.
	RefundedTotal float64 `json:"refunded_total,omitempty"`
}

func (ChargeRefunded) EventName() string  { return "ChargeRefunded" }
func (ChargeRefunded) SchemaVersion() int { return 1 }

// SubscriptionGraceStarted marca el inicio de periodo de gracia tras impago.
type SubscriptionGraceStarted struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	GraceEndsAt    time.Time `json:"grace_ends_at"`
	At             time.Time `json:"at"`
}

func (SubscriptionGraceStarted) EventName() string  { return "SubscriptionGraceStarted" }
func (SubscriptionGraceStarted) SchemaVersion() int { return 1 }

// SubscriptionRestricted restringe los permisos de escritura de una suscripción.
type SubscriptionRestricted struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (SubscriptionRestricted) EventName() string  { return "SubscriptionRestricted" }
func (SubscriptionRestricted) SchemaVersion() int { return 1 }

// SubscriptionReactivated reactiva la suscripción completa tras resolver impago o bloqueo.
type SubscriptionReactivated struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	At             time.Time `json:"at"`
}

func (SubscriptionReactivated) EventName() string  { return "SubscriptionReactivated" }
func (SubscriptionReactivated) SchemaVersion() int { return 1 }

// SubscriptionCancellationScheduled programa la cancelación al finalizar el periodo actual.
type SubscriptionCancellationScheduled struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	EffectiveAt    time.Time `json:"effective_at"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (SubscriptionCancellationScheduled) EventName() string {
	return "SubscriptionCancellationScheduled"
}
func (SubscriptionCancellationScheduled) SchemaVersion() int { return 1 }

// SubscriptionCancelled cancela la suscripción con efecto inmediato.
type SubscriptionCancelled struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (SubscriptionCancelled) EventName() string  { return "SubscriptionCancelled" }
func (SubscriptionCancelled) SchemaVersion() int { return 1 }

// SubscriptionResumed anula una cancelación programada restaurando renovación normal.
type SubscriptionResumed struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	At             time.Time `json:"at"`
}

func (SubscriptionResumed) EventName() string  { return "SubscriptionResumed" }
func (SubscriptionResumed) SchemaVersion() int { return 1 }

// TrialExpired se emite cuando el periodo de prueba concluye sin contratación.
type TrialExpired struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	GraceEndsAt    time.Time `json:"grace_ends_at"`
	At             time.Time `json:"at"`
}

func (TrialExpired) EventName() string  { return "TrialExpired" }
func (TrialExpired) SchemaVersion() int { return 1 }

// AdminAdjustmentApplied registra modificaciones manuales realizadas por SuperAdmin.
type AdminAdjustmentApplied struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	Kind           string    `json:"kind"` // extend_trial, credit, comp_plan
	Details        string    `json:"details"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
	// Efectos estructurados del ajuste (aditivos, M5.2).
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
	Credit      float64    `json:"credit,omitempty"`
}

func (AdminAdjustmentApplied) EventName() string  { return "AdminAdjustmentApplied" }
func (AdminAdjustmentApplied) SchemaVersion() int { return 1 }

// CouponAppliedEvent registra la aplicación de un cupón de descuento en la suscripción.
type CouponAppliedEvent struct {
	SubscriptionID uuid.UUID     `json:"subscription_id"`
	Coupon         CouponApplied `json:"coupon"`
	At             time.Time     `json:"at"`
}

func (CouponAppliedEvent) EventName() string  { return "CouponAppliedEvent" }
func (CouponAppliedEvent) SchemaVersion() int { return 1 }
