package domain

import "github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"

// Errores de dominio de Organization (catálogo API-011).
var (
	// ErrOrgRejected: una organización rechazada no admite cambios.
	ErrOrgRejected = kernel.NewDomainError("ORGANIZATION_REJECTED", "La organización fue rechazada.")

	// ErrOrgSuspended: no se puede editar una organización suspendida.
	ErrOrgSuspended = kernel.NewDomainError("ORGANIZATION_SUSPENDED", "La organización está suspendida.")

	// ErrInvalidStatusTransition: transición de estado no permitida.
	ErrInvalidStatusTransition = kernel.NewDomainError("INVALID_STATUS_TRANSITION", "Transición de estado no permitida.")

	// ErrRoleNotAllowedForType: O1 — rol inválido para el tipo (422 ROLE_NOT_ALLOWED_FOR_TYPE).
	ErrRoleNotAllowedForType = kernel.NewDomainError("ROLE_NOT_ALLOWED_FOR_TYPE", "El rol no es válido para este tipo de organización.")

	// ErrLastAdmin: O2 — no degradar/retirar al último admin activo (409 LAST_ADMIN_CANNOT_BE_REMOVED).
	ErrLastAdmin = kernel.NewDomainError("LAST_ADMIN_CANNOT_BE_REMOVED", "No se puede retirar ni degradar al último administrador.")

	// ErrAlreadyMember: el email ya es miembro activo.
	ErrAlreadyMember = kernel.NewDomainError("ALREADY_MEMBER", "Ese email ya es miembro de la organización.")

	// ErrInvitationNotFound: sin invitación pendiente para ese id/email.
	ErrInvitationNotFound = kernel.NewDomainError("INVITATION_NOT_FOUND", "No hay una invitación pendiente.")

	// ErrInvitationTokenInvalid: token inválido o caducado (HTTP → 404 sin distinguir).
	ErrInvitationTokenInvalid = kernel.NewDomainError("INVITATION_TOKEN_INVALID", "Invitación inválida o caducada.")

	// ErrInvitationEmailMismatch: el email del actor no coincide con la invitación.
	ErrInvitationEmailMismatch = kernel.NewDomainError("INVITATION_EMAIL_MISMATCH", "La invitación está dirigida a otro email.")

	// ErrEmailNotVerified: aceptar invitación exige email verificado.
	ErrEmailNotVerified = kernel.NewDomainError("EMAIL_NOT_VERIFIED", "Debes verificar tu email para aceptar.")

	// ErrMemberNotFound: el usuario no es miembro activo.
	ErrMemberNotFound = kernel.NewDomainError("MEMBER_NOT_FOUND", "El usuario no es miembro de la organización.")

	// ErrTaxIDAlreadyRegistered: reserva de tax_id tomada (422 ALREADY_REGISTERED).
	ErrTaxIDAlreadyRegistered = kernel.NewDomainError("ALREADY_REGISTERED", "Ya existe una organización con esa identificación fiscal.")

	// ErrPublicCodeTaken: reserva de public_code tomada (casi imposible; reintento).
	ErrPublicCodeTaken = kernel.NewDomainError("PUBLIC_CODE_TAKEN", "Código público no disponible.")
)

// ErrNoChanges: UpdateOrganizationProfile sin cambios reales no emite evento.
var ErrNoChanges = kernel.NewDomainError("NO_CHANGES", "No hay cambios que aplicar.")
