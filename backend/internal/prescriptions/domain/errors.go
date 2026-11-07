package domain

import "github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"

var (
	ErrPrescriptionNotFound    = kernel.NewDomainError("PRESCRIPTION_NOT_FOUND", "Receta no encontrada.")
	ErrCredentialRequired      = kernel.NewDomainError("CREDENTIAL_REQUIRED", "El veterinario requiere una credencial profesional verificada en el país para emitir recetas.")
	ErrCredentialExpired       = kernel.NewDomainError("CREDENTIAL_EXPIRED", "La credencial profesional del veterinario ha caducado.")
	ErrPrescriptionIncomplete  = kernel.NewDomainError("PRESCRIPTION_INCOMPLETE", "La receta debe contener al menos un medicamento válido.")
	ErrValidityOutOfRules      = kernel.NewDomainError("VALIDITY_OUT_OF_RULES", "La vigencia o número de refills supera los límites regulatorios permitidos para el país.")
	ErrItemInvalid             = kernel.NewDomainError("ITEM_INVALID", "Los datos del medicamento en la receta no cumplen las reglas de negocio.")
	ErrPrescriptionNotDraft    = kernel.NewDomainError("PRESCRIPTION_NOT_DRAFT", "Solo se pueden modificar o emitir recetas en estado borrador.")
	ErrPrescriptionDispensed   = kernel.NewDomainError("PRESCRIPTION_DISPENSED", "No se puede revocar una receta que ya registra dispensaciones.")
	ErrPrescriptionNotQuotable = kernel.NewDomainError("PRESCRIPTION_NOT_QUOTABLE", "La receta no admite cotizaciones en su estado o vigencia actual.")
	ErrDispensationExceeds     = kernel.NewDomainError("DISPENSATION_EXCEEDS", "La cantidad a dispensar supera el saldo pendiente de la receta.")
	ErrRefillsExceeded         = kernel.NewDomainError("REFILLS_EXCEEDED", "Se han agotado los refills permitidos para esta receta.")
	ErrPrescriptionImmutable   = kernel.NewDomainError("PRESCRIPTION_IMMUTABLE", "Una receta emitida o confirmada es inmutable salvo dispensación o revocación.")
	ErrPrescriptionExpired     = kernel.NewDomainError("PRESCRIPTION_EXPIRED", "La receta ha caducado y no admite nuevas operaciones.")
	ErrOrgAccessDenied         = kernel.NewDomainError("ORGANIZATION_ACCESS_DENIED", "La clínica no tiene acceso activo a la historia de la mascota.")
	ErrEncounterNotSigned      = kernel.NewDomainError("ENCOUNTER_NOT_SIGNED", "El encuentro clínico origen debe estar firmado y pertenecer a la misma clínica y mascota.")
	ErrExternalUploadLimit     = kernel.NewDomainError("EXTERNAL_UPLOAD_LIMIT", "Una receta externa admite un máximo de 5 páginas.")
	ErrQuoteRequestOpen        = kernel.NewDomainError("QUOTE_REQUEST_OPEN", "No se puede reabrir o modificar una receta con solicitudes de cotización abiertas.")
)
