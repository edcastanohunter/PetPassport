package entitlements

import (
	"fmt"
	"time"
)

// RestrictedError se produce cuando una organización restringida intenta ejecutar comandos de escritura (PP-F5-API-012, 402).
type RestrictedError struct {
	Reason      string     `json:"reason"`
	GraceEndsAt *time.Time `json:"grace_ends_at,omitempty"`
	BillingURL  string     `json:"billing_url"`
}

func (e RestrictedError) Error() string {
	return fmt.Sprintf("suscripción restringida: %s", e.Reason)
}

// PlanLimitReachedError se produce al exceder una cuota, asiento o capacidad del plan (PP-F5-API-012, 422).
type PlanLimitReachedError struct {
	Quota      string     `json:"quota"`
	Limit      int64      `json:"limit"`
	Used       int64      `json:"used"`
	PeriodEnd  *time.Time `json:"period_end,omitempty"`
	UpgradeURL string     `json:"upgrade_url"`
}

func (e PlanLimitReachedError) Error() string {
	return fmt.Sprintf("límite del plan alcanzado para %s (límite: %d, usado: %d)", e.Quota, e.Limit, e.Used)
}
