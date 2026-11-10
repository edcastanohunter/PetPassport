package kernel

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EntitlementStatus representa el estado operativo de la organización (ADR-025).
type EntitlementStatus string

const (
	EntitlementActive     EntitlementStatus = "active"
	EntitlementGrace      EntitlementStatus = "grace"
	EntitlementRestricted EntitlementStatus = "restricted"
	EntitlementTrialing   EntitlementStatus = "trialing"
	EntitlementExpired    EntitlementStatus = "expired"
	EntitlementCancelled  EntitlementStatus = "cancelled"
)

// QuotaKey identifica un límite de volumen medido por periodo.
type QuotaKey string

const (
	QuotaQuotesPerMonth QuotaKey = "quotes_per_month"
	QuotaSMSPerMonth    QuotaKey = "sms_per_month"
	QuotaStorageGB      QuotaKey = "storage_gb"
)

// Quota define los límites y consumo de un recurso medible.
type Quota struct {
	Limit     int64      `json:"limit"`
	Used      int64      `json:"used"`
	SoftLimit int64      `json:"soft_limit"`
	PeriodEnd *time.Time `json:"period_end,omitempty"`
}

// SeatAllocation define los asientos asignados y utilizados.
type SeatAllocation struct {
	Limit int `json:"limit"`
	Used  int `json:"used"`
}

// EntitlementSet es el conjunto efectivo de límites y capacidades de una organización (ADR-025).
type EntitlementSet struct {
	OrgID          uuid.UUID          `json:"organization_id"`
	AccountID      *uuid.UUID         `json:"account_id,omitempty"`
	SubscriptionID *uuid.UUID         `json:"subscription_id,omitempty"`
	PlanCode       string             `json:"plan_code"`
	PlanVersion    int                `json:"plan_version"`
	Status         EntitlementStatus  `json:"status"`
	Seats          SeatAllocation     `json:"seats"`
	Quotas         map[QuotaKey]Quota `json:"quotas"`
	Features       []string           `json:"features"`
	PeriodEnd      *time.Time         `json:"period_end,omitempty"`
	ComputedAt     time.Time          `json:"computed_at"`
}

// HasFeature comprueba si una capacidad está habilitada en el plan.
func (e EntitlementSet) HasFeature(name string) bool {
	for _, f := range e.Features {
		if f == name {
			return true
		}
	}
	return false
}

// RequirementKind clasifica la restricción impuesta por un comando.
type RequirementKind string

const (
	ReqKindSeat    RequirementKind = "seat"
	ReqKindQuota   RequirementKind = "quota"
	ReqKindFeature RequirementKind = "feature"
)

// Requirement es la declaración de consumo que hace un comando (PP-F5-ARQ-004).
type Requirement struct {
	Kind   RequirementKind `json:"kind"`
	Key    string          `json:"key"`
	Amount int64           `json:"amount"`
}

// SeatReq declara el consumo o verificación de un asiento.
func SeatReq() Requirement {
	return Requirement{Kind: ReqKindSeat, Amount: 1}
}

// QuotaReq declara el consumo de una cuota de volumen por periodo.
func QuotaReq(key QuotaKey, amount int64) Requirement {
	return Requirement{Kind: ReqKindQuota, Key: string(key), Amount: amount}
}

// FeatureReq declara la necesidad de una capacidad activable por plan.
func FeatureReq(feature string) Requirement {
	return Requirement{Kind: ReqKindFeature, Key: feature}
}

// ConsumingCommand es implementado por cualquier comando que consume recursos o capacidades de plan.
type ConsumingCommand interface {
	Consumes() Requirement
	TargetOrgID() uuid.UUID
}

// Entitlements es el puerto primario consultado por todos los contextos de negocio (PP-F5-DOM-011).
type Entitlements interface {
	For(ctx context.Context, orgID uuid.UUID) (EntitlementSet, error)
}

// OrgRoster es el puerto para consultar profesionales activos en una organización (PP-F5-DOM-011).
type OrgRoster interface {
	ActiveProfessionals(ctx context.Context, orgID uuid.UUID) (int, error)
}

// FakeEntitlements es una implementación en memoria para pruebas.
type FakeEntitlements struct {
	mu           sync.RWMutex
	entitlements map[uuid.UUID]EntitlementSet
	defaultSet   func(orgID uuid.UUID) EntitlementSet
}

// NewFakeEntitlements crea un FakeEntitlements configurable.
func NewFakeEntitlements() *FakeEntitlements {
	return &FakeEntitlements{
		entitlements: make(map[uuid.UUID]EntitlementSet),
		defaultSet: func(orgID uuid.UUID) EntitlementSet {
			return EntitlementSet{
				OrgID:       orgID,
				PlanCode:    "starter",
				PlanVersion: 1,
				Status:      EntitlementActive,
				Seats:       SeatAllocation{Limit: 2, Used: 0},
				Quotas: map[QuotaKey]Quota{
					QuotaQuotesPerMonth: {Limit: 50, Used: 0, SoftLimit: 55},
				},
				Features:   []string{"agenda", "encounters", "vaccination_cards", "prescriptions", "quotes"},
				ComputedAt: time.Now().UTC(),
			}
		},
	}
}

// Set fija los entitlements para una organización.
func (f *FakeEntitlements) Set(orgID uuid.UUID, set EntitlementSet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entitlements[orgID] = set
}

// For implementa Entitlements.
func (f *FakeEntitlements) For(ctx context.Context, orgID uuid.UUID) (EntitlementSet, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if set, ok := f.entitlements[orgID]; ok {
		return set, nil
	}
	return f.defaultSet(orgID), nil
}
