package domain

import "github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"

// Errores de dominio del agregado Pet (catálogo API-011). Centinelas: se
// comparan con errors.Is y mapean a problem+json vía kernel.DomainError.
var (
	// ErrPetDeceased: I3 — una mascota fallecida solo admite RevokeCareLink,
	// UndoDeceased y ChangePetPhoto.
	ErrPetDeceased = kernel.NewDomainError("PET_DECEASED", "La mascota está fallecida.")

	// ErrPetNotDeceased: deshacer fallecimiento cuando está activa.
	ErrPetNotDeceased = kernel.NewDomainError("PET_NOT_DECEASED", "La mascota no está fallecida.")

	// ErrUndoWindowExpired: la ventana de 24 h cerró; requiere super_admin.
	ErrUndoWindowExpired = kernel.NewDomainError("UNDO_WINDOW_EXPIRED", "La ventana de 24 horas para deshacer ha expirado.")

	// ErrMicrochipCorrectionLimit: I7 — el tutor ya corrigió una vez.
	ErrMicrochipCorrectionLimit = kernel.NewDomainError("MICROCHIP_CORRECTION_LIMIT", "El microchip ya se corrigió una vez; contacta a soporte.")

	// ErrMicrochipSameValue: asignar el microchip que ya tenía no es cambio.
	ErrMicrochipSameValue = kernel.NewDomainError("MICROCHIP_SAME_VALUE", "La mascota ya tiene ese microchip.")

	// ErrCareLinkLimit: I8 — máximo 20 vínculos activos.
	ErrCareLinkLimit = kernel.NewDomainError("CARE_LINK_LIMIT", "Máximo 20 vínculos activos por mascota.")

	// ErrCareLinkNotActive: revocar un vínculo que no está activo.
	ErrCareLinkNotActive = kernel.NewDomainError("CARE_LINK_NOT_ACTIVE", "El vínculo no está activo.")

	// ErrTransferAlreadyPending: ya hay transferencia pendiente.
	ErrTransferAlreadyPending = kernel.NewDomainError("TRANSFER_ALREADY_PENDING", "Ya hay una transferencia pendiente.")

	// ErrTransferNotPending: cancelar/aceptar sin transferencia pendiente.
	ErrTransferNotPending = kernel.NewDomainError("TRANSFER_NOT_PENDING", "No hay transferencia pendiente.")

	// ErrTransferTokenInvalid: token no coincide o caducó (HTTP lo resuelve
	// como 404 sin distinguir, API-004).
	ErrTransferTokenInvalid = kernel.NewDomainError("TRANSFER_TOKEN_INVALID", "Token inválido o caducado.")

	// ErrTransferEmailMismatch: el actor no es el destinatario.
	ErrTransferEmailMismatch = kernel.NewDomainError("TRANSFER_EMAIL_MISMATCH", "La transferencia no está dirigida a tu email.")

	// ErrSameGuardian: I4 — el nuevo tutor es el actual.
	ErrSameGuardian = kernel.NewDomainError("SAME_GUARDIAN", "El nuevo tutor es el actual.")

	// ErrOrganizationNotEligible: I5 — la org no es clínica activa (se evalúa
	// en app con el puerto OrganizationLookup).
	ErrOrganizationNotEligible = kernel.NewDomainError("ORGANIZATION_NOT_ELIGIBLE", "La organización no puede recibir vínculos.")
)

// ErrMicrochipAlreadyAssigned lo produce la reserva transaccional (DAT-005):
// el microchip pertenece a otra mascota; sin revelar a cuál (PRD-007).
var ErrMicrochipAlreadyAssigned = kernel.NewDomainError("MICROCHIP_ALREADY_ASSIGNED", "El microchip ya está asignado a otra mascota.")

// ErrNoChanges: UpdatePetProfile sin cambios reales no emite evento.
var ErrNoChanges = kernel.NewDomainError("NO_CHANGES", "No hay cambios que aplicar.")
