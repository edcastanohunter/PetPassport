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
	PetRegister        Action = "pet:register"
	PetRead            Action = "pet:read"
	PetUpdate          Action = "pet:update"
	PetMicrochip       Action = "pet:microchip"
	PetPhoto           Action = "pet:photo"
	PetDeceased        Action = "pet:deceased"
	PetTransfer        Action = "pet:transfer"
	PetCareLinkGrant   Action = "pet:carelink:grant"
	PetCareLinkRevoke  Action = "pet:carelink:revoke"
	PetGuardianContact Action = "pet:guardian_contact:read"
	// Aceptación/previsualización de transferencia: el receptor aún no es G;
	// basta estar autenticado (el dominio verifica que el email coincide).
	PetTransferRead   Action = "pet:transfer:read"
	PetTransferAccept Action = "pet:transfer:accept"
)

// Acciones sobre organizaciones (IAM-010, filas org:* ; M1.3).
const (
	OrgRegister       Action = "org:register"
	OrgRead           Action = "org:read"
	OrgLookup         Action = "org:lookup"
	OrgUpdate         Action = "org:update"
	OrgMemberInvite   Action = "org:member:invite"
	OrgMemberRole     Action = "org:member:role"
	OrgMemberRemove   Action = "org:member:remove"
	OrgPatientsList   Action = "org:patients:list"
	OrgInvitationRead Action = "org:invitation:read"
	OrgInvitationJoin Action = "org:invitation:accept"
)

// Acciones de administración de plataforma (IAM-010 última fila, API-008).
// Solo super_admin; los comandos exigen motivo y se auditan (RN-06).
const (
	AdminOrgList        Action = "admin:org:list"
	AdminOrgVerify      Action = "admin:org:verify"
	AdminOrgReject      Action = "admin:org:reject"
	AdminOrgSuspend     Action = "admin:org:suspend"
	AdminOrgReactivate  Action = "admin:org:reactivate"
	AdminUserList       Action = "admin:user:list"
	AdminUserSuspend    Action = "admin:user:suspend"
	AdminUserReactivate Action = "admin:user:reactivate"
	AdminAuditRead      Action = "admin:audit:read"
)

// ReasonOrgSuspended es el motivo de denegación de org:patients:list cuando
// la organización del miembro está suspendida (→ 403 ORGANIZATION_SUSPENDED).
const ReasonOrgSuspended = "organization suspended"

// OrgResource es la vista de autorización de una organización. MembersOnly
// exige membresía (p. ej. lista de miembros); sin él, org:read admite la
// vista pública mínima para cualquier autenticado.
type OrgResource struct {
	ID          uuid.UUID
	MembersOnly bool
}

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
	"clinic_admin": true,
	"veterinarian": true,
	"receptionist": true,
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

// ---------------------------------------------------------- organizaciones

// adminRoles administran su organización (org:update, org:member:*).
var adminRoles = map[string]bool{"clinic_admin": true, "pharmacy_admin": true}

func orgResource(res Resource) (*OrgResource, bool) {
	r, ok := res.(OrgResource)
	if !ok || r.ID == uuid.Nil {
		return nil, false
	}
	return &r, true
}

// membership devuelve la membresía activa del principal en la org.
func membership(p auth.Principal, orgID uuid.UUID) (auth.Membership, bool) {
	for _, m := range p.Memberships {
		if m.OrganizationID == orgID {
			return m, true
		}
	}
	return auth.Membership{}, false
}

// orgRead: cualquier autenticado ve la vista pública mínima; con
// MembersOnly (miembros, invitaciones de lectura) exige membresía o super_admin.
func orgRead(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := orgResource(res)
	if !ok {
		return deny("organization resource required")
	}
	if !r.MembersOnly {
		return allow
	}
	if _, member := membership(p, r.ID); member {
		return allow
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("not a member")
}

// orgAdmin: admin de la propia organización o super_admin (org:update,
// org:member:invite/role/remove). El estado de la org lo valida el dominio.
func orgAdmin(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := orgResource(res)
	if !ok {
		return deny("organization resource required")
	}
	if m, member := membership(p, r.ID); member && adminRoles[m.Role] {
		return allow
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("not an organization admin")
}

// orgPatientsList: miembro de clínica con rol de clínica y org activa, o
// super_admin. Org suspendida → ReasonOrgSuspended (403 ORGANIZATION_SUSPENDED).
func orgPatientsList(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := orgResource(res)
	if !ok {
		return deny("organization resource required")
	}
	if m, member := membership(p, r.ID); member && m.OrgType == "clinic" && clinicReaderRoles[m.Role] {
		switch m.OrgStatus {
		case "active":
			return allow
		case "suspended":
			return deny(ReasonOrgSuspended)
		}
		return deny("organization not active")
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("not a clinic member")
}

// superAdminOnly: acciones de administración de plataforma (admin:*).
func superAdminOnly(p auth.Principal, _ Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("super_admin required")
}

var policies = map[Action]Policy{
	MeRead:             authenticatedOnly,
	MeUpdate:           authenticatedOnly,
	OnboardingComplete: authenticatedOnly,

	PetRegister:        petRegister,
	PetRead:            petRead,
	PetUpdate:          petManage,
	PetMicrochip:       petManage,
	PetPhoto:           petManage,
	PetDeceased:        petManage,
	PetTransfer:        petManage,
	PetCareLinkGrant:   petManage,
	PetCareLinkRevoke:  petManage,
	PetGuardianContact: petGuardianContactRead,
	PetTransferRead:    authenticatedOnly,
	PetTransferAccept:  authenticatedOnly,

	OrgRegister:       authenticatedOnly,
	OrgRead:           orgRead,
	OrgLookup:         authenticatedOnly,
	OrgUpdate:         orgAdmin,
	OrgMemberInvite:   orgAdmin,
	OrgMemberRole:     orgAdmin,
	OrgMemberRemove:   orgAdmin,
	OrgPatientsList:   orgPatientsList,
	OrgInvitationRead: authenticatedOnly,
	OrgInvitationJoin: authenticatedOnly,

	AdminOrgList:        superAdminOnly,
	AdminOrgVerify:      superAdminOnly,
	AdminOrgReject:      superAdminOnly,
	AdminOrgSuspend:     superAdminOnly,
	AdminOrgReactivate:  superAdminOnly,
	AdminUserList:       superAdminOnly,
	AdminUserSuspend:    superAdminOnly,
	AdminUserReactivate: superAdminOnly,
	AdminAuditRead:      superAdminOnly,
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
