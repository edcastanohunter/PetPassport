// Package authz decide si un principal puede ejecutar una acción sobre un
// recurso. Deny by default; políticas explícitas y probadas una a una
// (docs/phase-1/04 §8 IAM-010). En M1.1 solo existen las acciones de cuenta
// propia; la tabla completa (pets, organizaciones, admin) llega en M1.2–M1.4.
package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Action es una acción del catálogo (coincide con `x-pp-action` del contrato).
type Action string

// Acciones de M1.1.
const (
	MeRead             Action = "me:read"
	MeUpdate           Action = "me:update"
	OnboardingComplete Action = "onboarding:complete"
)

// Resource es el recurso sobre el que se actúa; nil para acciones sin recurso.
type Resource any

// Decision es el resultado de una política.
type Decision struct {
	Allow  bool
	Reason string
}

// Policy decide para una acción.
type Policy func(p auth.Principal, res Resource) Decision

var allow = Decision{Allow: true}

func deny(reason string) Decision { return Decision{Allow: false, Reason: reason} }

// authenticatedOnly: cualquier principal autenticado puede actuar sobre su propia cuenta.
func authenticatedOnly(p auth.Principal, _ Resource) Decision {
	if p.UserID == uuid.Nil {
		return deny("unauthenticated")
	}
	if p.Account.Suspended {
		return deny("account suspended")
	}
	return allow
}

var policies = map[Action]Policy{
	MeRead:             authenticatedOnly,
	MeUpdate:           authenticatedOnly,
	OnboardingComplete: authenticatedOnly,
}

// Can evalúa la política de la acción. Acciones sin política → deny.
func Can(p auth.Principal, action Action, res Resource) Decision {
	pol, ok := policies[action]
	if !ok {
		return deny("no policy for action " + string(action))
	}
	return pol(p, res)
}

// Register añade una política (la usan los módulos al arrancar). Pánico si ya existe.
func Register(action Action, pol Policy) {
	if _, exists := policies[action]; exists {
		panic("authz: policy already registered for " + string(action))
	}
	policies[action] = pol
}

// Actions devuelve las acciones con política (para el test de cobertura de rutas).
func Actions() []Action {
	out := make([]Action, 0, len(policies))
	for a := range policies {
		out = append(out, a)
	}
	return out
}
