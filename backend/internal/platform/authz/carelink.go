package authz

import "github.com/edcastanohunter/petpassport/backend/internal/platform/auth"

// Acciones de la solicitud de vínculo y de las estadísticas de clínica
// (PP-F2-SEC-001, docs/phase-2/08 §2; API-009, API-011).
const (
	// CareLinkRequest: miembro clínico activo solicita el vínculo (PRD-020).
	// No es una acción de super_admin: el vínculo lo pide la clínica.
	CareLinkRequest Action = "carelink:request"
	// PetAlerts: el tutor o una clínica con vínculo activo edita las alertas
	// del perfil (DOM-013). Misma regla que pet:read.
	PetAlerts Action = "pet:alerts"
	// OrgStatsRead: administrador o veterinario de la clínica (PRD-026).
	OrgStatsRead Action = "org:stats:read"
)

// carelinkRequest: miembro de una clínica activa (rol de clínica).
func carelinkRequest(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := orgResource(res)
	if !ok {
		return deny("organization resource required")
	}
	if _, ok := activeClinicMember(p, r.ID); ok {
		return allow
	}
	return deny("active clinic member required")
}

// orgStatsRead: clinic_admin o veterinarian de la clínica activa, o super_admin.
func orgStatsRead(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := orgResource(res)
	if !ok {
		return deny("organization resource required")
	}
	if m, ok := activeClinicMember(p, r.ID); ok && (m.Role == "clinic_admin" || m.Role == "veterinarian") {
		return allow
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	return deny("clinic admin or veterinarian required")
}

func init() {
	policies[CareLinkRequest] = carelinkRequest
	policies[PetAlerts] = petRead
	policies[OrgStatsRead] = orgStatsRead
}
