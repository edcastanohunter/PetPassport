package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Acciones de Prescriptions F4 (PP-F4-SEC-006, SEC-007, API-002, API-004).
const (
	PrescriptionIssue  Action = "prescription:issue"
	PrescriptionRevoke Action = "prescription:revoke"
	PrescriptionRead   Action = "prescription:read"
	PrescriptionList   Action = "prescription:list"
	PrescriptionUpload Action = "prescription:upload"
)

// PrescriptionResource es la vista de autorización de una receta.
type PrescriptionResource struct {
	ID          uuid.UUID
	PetID       uuid.UUID
	GuardianID  uuid.UUID
	IssuerOrgID *uuid.UUID
	IssuerID    *uuid.UUID
}

func prescriptionResource(res Resource) (*PrescriptionResource, bool) {
	r, ok := res.(PrescriptionResource)
	if !ok || (r.ID == uuid.Nil && r.IssuerOrgID == nil && r.GuardianID == uuid.Nil) {
		return nil, false
	}
	return &r, true
}

func init() {
	Register(PrescriptionIssue, prescriptionIssuePolicy)
	Register(PrescriptionRevoke, prescriptionRevokePolicy)
	Register(PrescriptionRead, prescriptionReadPolicy)
	Register(PrescriptionList, prescriptionListPolicy)
	Register(PrescriptionUpload, prescriptionUploadPolicy)
}

// prescriptionIssuePolicy: solo veterinarios de clínica activa o super_admin.
func prescriptionIssuePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		m, ok := activeClinicMember(p, o.ID)
		if !ok {
			return deny("active clinic member required")
		}
		if !isVetMembership(m) {
			return deny("veterinarian role required")
		}
		return allow
	}
	r, ok := prescriptionResource(res)
	if !ok || r.IssuerOrgID == nil {
		return deny("prescription resource required")
	}
	m, ok := activeClinicMember(p, *r.IssuerOrgID)
	if !ok {
		return deny("active clinic member required")
	}
	if !isVetMembership(m) {
		return deny("veterinarian role required")
	}
	return allow
}

// prescriptionRevokePolicy: veterinario de la org emisora, admin de la org emisora, o super_admin.
func prescriptionRevokePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	r, ok := prescriptionResource(res)
	if !ok || r.IssuerOrgID == nil {
		return deny("prescription resource with issuer org required")
	}
	m, ok := activeClinicMember(p, *r.IssuerOrgID)
	if !ok {
		return deny("active clinic member required")
	}
	if isVetMembership(m) || m.Role == "clinic_admin" {
		return allow
	}
	return deny("vet or clinic_admin required")
}

// prescriptionListPolicy: miembros activos de la organización o super_admin.
func prescriptionListPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		if _, ok := activeClinicMember(p, o.ID); ok {
			return allow
		}
		return deny("active clinic member required")
	}
	if r, ok := res.(PrescriptionResource); ok && r.IssuerOrgID != nil {
		if _, ok := activeClinicMember(p, *r.IssuerOrgID); ok {
			return allow
		}
		return deny("active clinic member required")
	}
	return deny("org resource required")
}

// prescriptionReadPolicy: tutor de la mascota, miembro de la org emisora o super_admin.
func prescriptionReadPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	r, ok := prescriptionResource(res)
	if !ok {
		return deny("prescription resource required")
	}
	if p.UserID == r.GuardianID {
		return allow
	}
	if r.IssuerOrgID != nil {
		if _, ok := activeClinicMember(p, *r.IssuerOrgID); ok {
			return allow
		}
	}
	return deny("not authorized to read prescription")
}

// prescriptionUploadPolicy: tutor de la mascota o super_admin.
func prescriptionUploadPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if r, ok := res.(PrescriptionResource); ok && r.GuardianID != uuid.Nil {
		if p.UserID == r.GuardianID {
			return allow
		}
		return deny("guardian only")
	}
	if pet, ok := res.(PetResource); ok && pet.GuardianID != uuid.Nil {
		if p.UserID == pet.GuardianID {
			return allow
		}
		return deny("guardian only")
	}
	return deny("pet or prescription resource required")
}
