package domain

import (
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	// Invariantes de QuoteRequest (Q1–Q8)
	ErrNotGuardian                  = kernel.NewDomainError("NOT_GUARDIAN", "El usuario no es el tutor de la receta.")
	ErrPrescriptionNotQuotable      = kernel.NewDomainError("PRESCRIPTION_NOT_QUOTABLE", "La receta no está vigente o no es cotizable.")
	ErrControlledNotOrderable       = kernel.NewDomainError("CONTROLLED_NOT_ORDERABLE", "Las sustancias controladas no son cotizables ni pedibles.")
	ErrItemsNotSubset               = kernel.NewDomainError("ITEMS_NOT_SUBSET", "Los ítems solicitados deben pertenecer a la receta.")
	ErrItemsEmpty                   = kernel.NewDomainError("ITEMS_EMPTY", "La solicitud debe incluir al menos un ítem.")
	ErrInvalidOrgCount              = kernel.NewDomainError("INVALID_ORG_COUNT", "Debe invitar entre 1 y 10 farmacias.")
	ErrPharmacyNotEligible          = kernel.NewDomainError("NO_ELIGIBLE_PHARMACIES", "Una o más farmacias no son elegibles o activas en la zona.")
	ErrQuoteRequestLimit            = kernel.NewDomainError("QUOTE_REQUEST_LIMIT", "Límite de solicitudes de cotización excedido.")
	ErrQuoteRequestClosed           = kernel.NewDomainError("QUOTE_REQUEST_CLOSED", "La solicitud de cotización ya no está abierta.")
	ErrOrgNotInvited                = kernel.NewDomainError("ORG_NOT_INVITED", "La farmacia no está invitada a esta cotización.")
	ErrSubstitutionNotAllowed       = kernel.NewDomainError("SUBSTITUTION_NOT_ALLOWED", "El ítem no permite sustitución de producto.")
	ErrSubstitutionNotEquivalent    = kernel.NewDomainError("SUBSTITUTION_NOT_EQUIVALENT", "El producto propuesto no es equivalente en principio activo, concentración y forma.")
	ErrPriceNegative                = kernel.NewDomainError("PRICE_NEGATIVE", "El precio unitario no puede ser negativo.")
	ErrQuoteExpired                 = kernel.NewDomainError("QUOTE_EXPIRED", "La oferta de cotización ha expirado.")
	ErrSubstitutionDecisionRequired = kernel.NewDomainError("SUBSTITUTION_DECISION_REQUIRED", "Debe aceptar o rechazar cada sustitución propuesta.")
	ErrQuoteNotFound                = kernel.NewDomainError("QUOTE_NOT_FOUND", "La cotización seleccionada no existe en esta solicitud.")

	// Invariantes de Order (O1–O8)
	ErrOrderAlreadyPlaced      = kernel.NewDomainError("ORDER_ALREADY_PLACED", "Ya existe un pedido generado para esta cotización.")
	ErrModeMismatch            = kernel.NewDomainError("MODE_MISMATCH", "La modalidad del pedido no coincide con la cotización.")
	ErrDeliveryAddressRequired = kernel.NewDomainError("DELIVERY_ADDRESS_REQUIRED", "La dirección de entrega es obligatoria para envíos a domicilio.")
	ErrContactPhoneRequired    = kernel.NewDomainError("CONTACT_PHONE_REQUIRED", "El teléfono de contacto es obligatorio.")
	ErrInvalidTransition       = kernel.NewDomainError("INVALID_TRANSITION", "Transición de estado de pedido no permitida.")
	ErrDispensationRequired    = kernel.NewDomainError("DISPENSATION_REQUIRED", "Se requiere el registro de dispensación para marcar entregado.")
	ErrDispensationEmpty       = kernel.NewDomainError("DISPENSATION_EMPTY", "El registro de dispensación no puede estar vacío.")
	ErrDispensationExceeds     = kernel.NewDomainError("DISPENSATION_EXCEEDS", "La cantidad dispensada supera la cantidad del pedido.")
	ErrOrderNotCancellable     = kernel.NewDomainError("ORDER_NOT_CANCELLABLE", "El pedido no se puede cancelar en su estado actual.")
	ErrCancellationTimeout     = kernel.NewDomainError("ORDER_CANCELLATION_TIMEOUT", "No se puede cancelar por el tutor tras más de 1 hora en preparación.")
)
