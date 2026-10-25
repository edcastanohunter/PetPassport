// Package authz decide si un principal puede ejecutar una acción sobre un
// recurso. Deny by default; políticas explícitas y probadas una a una
// (docs/phase-1/04 §8 IAM-010). Cada handler HTTP declara su `x-pp-action`
// del contrato y llama a Can antes de ejecutar el comando/consulta; el test
// de cobertura de rutas (SEC-005, M1.3) usa la sonda de decisiones.
package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Action es una acción del catálogo (coincide con `x-pp-action` del contrato).
type Action string

// Acciones de M1.1 (cuenta propia).
const (
	MeRead             Action = "me:read"
	MeUpdate           Action = "me:update"
	OnboardingComplete Action = "onboarding:complete"
)

// Acciones sobre mascotas (IAM-010, filas de pets).
const (
	PetRegister          Action = "pet:register"
	PetRead              Action = "pet:read"
	PetUpdate            Action = "pet:update"
	PetMicrochip         Action = "pet:microchip"
	PetPhoto             Action = "pet:photo"
	PetDeceased          Action = "pet:deceased"
	PetTransfer          Action = "pet:transfer"
	PetCareLinkGrant     Action = "pet:carelink:grant"
	PetCareLinkRevoke    Action = "pet:carelink:revoke"
	PetGuardianContact   Action = "pet:guardian_contact:read"
)

// Resource es el recurso sobre el que se actúa; nil para acciones sin recurso.
type Resource any

// PetResource es la vista de autorización de una mascota, cargada del read
// model por el handler antes de evaluar la política (IAM-010 reglas de
// implementación). G = tutor; L = org con CareLink activo y org activa.
type PetResource struct {
	ID                   uuid.UUID
	GuardianID           uuid.UUID
	ActiveCareLinkOrgIDs []uuid.UUID
}

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

// Roles de organización de clínica que reciben L (vínculo activo).
var clinicReaderRoles = map[string]bool{
	"clinic_admin":  true,
	"veterinarian":  true,
	"receptionist":  true,
}

func isSuperAdmin(p auth.Principal) bool { return p.HasRole("super_admin") }

// isGuardian: el principal es el tutor de la mascota (G).
func isGuardian(p auth.Principal, res *PetResource) bool { return p.UserID == res.GuardianID }

// hasActiveCareLink: L — miembro de una org con vínculo activo, org activa y
// rol de clínica. En M1.3 la membresía se carga en el Principal (IAM-008);
// las políticas ya la evalúan para que la suite de SEC-005 no cambie.
func hasActiveCareLink(p auth.Principal, res *PetResource) bool {
	if len(res.ActiveCareLinkOrgIDs) == 0 {
		return false
	}
	linked := make(map[uuid.UUID]bool, len(res.ActiveCareLinkOrgIDs))
	for _, id := range res.ActiveCareLinkOrgIDs {
		linked[id] = true
	}
	for _, m := range p.Memberships {
		if linked[m.OrganizationID] && m.OrgStatus == "active" && clinicReaderRoles[m.Role] {
			return true
		}
	}
	return false
}

func petResource(res Resource) (*PetResource, bool) {
	r, ok := res.(PetResource)
	if !ok {
		return nil, false
	}
	return &r, true
}

// petRegister: cualquier autenticado (todo usuario puede ser tutor).
func petRegister(p auth.Principal, _ Resource) Decision { return authenticatedOnly(p, nil) }

// petRead: G, L o super_admin (IAM-010).
func petRead(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := petResource(res)
	if !ok {
		return deny("pet resource required")
	}
	switch {
	case isGuardian(p, r):
		return allow
	case isSuperAdmin(p):
		return Decision{Allow: true, Reason: "super_admin"}
	case hasActiveCareLink(p, r):
		return Decision{Allow: true, Reason: "care link"}
	}
	return deny("not guardian nor linked")
}

// petManage: solo G o super_admin (update, microchip, photo, deceased,
// transfer, carelink:grant/revoke).
func petManage(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := petResource(res)
	if !ok {
		return deny("pet resource required")
	}
	switch {
	case isGuardian(p, r):
		return allow
	case isSuperAdmin(p):
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("only guardian")
}

// petGuardianContactRead: G, L o super_admin. Las lecturas de admin quedan
// auditadas (M1.4); la política es la misma que pet:read.
func petGuardianContactRead(p auth.Principal, res Resource) Decision { return petRead(p, res) }

var policies = map[Action]Policy{
	MeRead:             authenticatedOnly,
	MeUpdate:           authenticatedOnly,
	OnboardingComplete: authenticatedOnly,

	PetRegister: petRegister,
	PetRead:     petRead,
	PetUpdate:   petManage,
	PetMicrochip: petManage,
	PetPhoto:    petManage,
	PetDeceased: petManage,
	PetTransfer: petManage,
	PetCareLinkGrant:  petManage,
	PetCareLinkRevoke: petManage,
	PetGuardianContact: petGuardianContactRead,
}

// Can evalúa la política de la acción. Acciones sin política → deny.
// Toda decisión se cuenta en métricas y se notifica a la sonda (tests).
func Can(p auth.Principal, action Action, res Resource) Decision {
	pol, ok := policies[action]
	if !ok {
		return notify(p, action, deny("no policy for action "+string(action)))
	}
	return notify(p, action, pol(p, res))
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
