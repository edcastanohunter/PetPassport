package domain

import (
	"errors"
	"fmt"
)

// Errores de dominio de Facturación (PP-F5-API-012).
var (
	ErrAccountClosed          = errors.New("la cuenta de facturación está cerrada")
	ErrOrgAlreadyBilled       = errors.New("la organización ya pertenece a una cuenta de facturación")
	ErrOrgNotAttached         = errors.New("la organización no pertenece a esta cuenta")
	ErrActiveSubscription     = errors.New("la cuenta tiene suscripciones activas")
	ErrInvalidTaxID           = errors.New("identificación fiscal inválida")
	ErrLegalEntityIncomplete  = errors.New("datos de la entidad legal incompletos")
	ErrPaymentMethodRequired  = errors.New("se requiere un método de pago activo")
	ErrSeatsBelowUsage        = errors.New("los asientos contratados no pueden ser inferiores a los profesionales activos")
	ErrPlanNotAvailable       = errors.New("el plan solicitado no está disponible")
	ErrPlanChangePending      = errors.New("ya existe un cambio de plan pendiente para el fin de ciclo")
	ErrInvalidCycleTransition = errors.New("transición de estado de suscripción inválida")
	ErrChargeNotPending       = errors.New("el cargo no está en estado pendiente")
	ErrChargeNotPaid          = errors.New("el cargo no está pagado")
	ErrRefundExceeds          = errors.New("el importe del reembolso excede el saldo reembolsable")
	ErrSubscriptionRestricted = errors.New("suscripción restringida")
	ErrPlanLimitReached       = errors.New("límite del plan alcanzado")
	ErrTrialExpired           = errors.New("el periodo de prueba ha expirado")
	ErrDuplicateSubscription  = errors.New("la cuenta ya posee una suscripción para este tipo de servicio")
	ErrTrialNotAllowed        = errors.New("el plan no permite periodo de prueba o ya fue utilizado")
)

// LimitReachedError detalla qué recurso superó la cuota asignada.
type LimitReachedError struct {
	Quota      string `json:"quota"`
	Limit      int64  `json:"limit"`
	Used       int64  `json:"used"`
	UpgradeURL string `json:"upgrade_url"`
}

func (e LimitReachedError) Error() string {
	return fmt.Sprintf("límite alcanzado para %s: %d/%d", e.Quota, e.Used, e.Limit)
}
