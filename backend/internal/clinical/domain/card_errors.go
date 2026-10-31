package domain

import "github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"

// Errores de VaccinationCard (catálogo PP-F2-API-012).
var (
	// ErrCardEntryDuplicate: V3 — ya hay una entrada activa igual.
	ErrCardEntryDuplicate = kernel.NewDomainError("CARD_ENTRY_DUPLICATE", "Ya existe una entrada activa con el mismo producto y fecha.")

	// ErrVoidWindowExpired: V5 — pasaron más de 30 días y el actor no es super_admin.
	ErrVoidWindowExpired = kernel.NewDomainError("VOID_WINDOW_EXPIRED", "La ventana de anulación (30 días) ha vencido.")

	// ErrVoidNotAllowed: V5 — el actor no registró la entrada ni es super_admin ni tutor de una declarada.
	ErrVoidNotAllowed = kernel.NewDomainError("VOID_NOT_ALLOWED", "No puedes anular esta entrada.")

	// ErrEntryNotActive: V4 — la entrada ya está anulada.
	ErrEntryNotActive = kernel.NewDomainError("ENTRY_NOT_ACTIVE", "La entrada ya está anulada.")

	// ErrEntryNotDeclared: verificar/disputar una entrada que no es declarada.
	ErrEntryNotDeclared = kernel.NewDomainError("ENTRY_NOT_DECLARED", "La entrada no fue declarada por el tutor.")

	// ErrCardFull: V6 — más de 500 entradas.
	ErrCardFull = kernel.NewDomainError("CARD_ENTRIES_LIMIT", "El carnet alcanzó el máximo de entradas.")

	// ErrGuardianRequired: declarar exige ser tutor de la mascota.
	ErrGuardianRequired = kernel.NewDomainError("GUARDIAN_REQUIRED", "Solo el tutor puede declarar entradas.")
)
