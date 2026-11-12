package problem

import (
	"context"
	"errors"
	"net/http"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// DirectError envuelve un Problem ya construido (p. ej. errores del pipeline
// de imagen) para saltar el mapeo de From.
type DirectError struct{ P *Problem }

func (e *DirectError) Error() string { return e.P.Title }

// From mapea las tres familias de errores de ARQ-010 a un Problem:
// DirectError → tal cual; DomainError → código del catálogo; NotFoundError → 404;
// ForbiddenError → 403; ValidationError → 422 con errors[]; cualquier otro →
// 500 sin detalles internos.
func From(err error) *Problem {
	var de *kernel.DomainError
	var nf *kernel.NotFoundError
	var fb *kernel.ForbiddenError
	var ve *kernel.ValidationError
	var ce *kernel.ConcurrencyError
	var direct *DirectError
	switch {
	case errors.As(err, &direct):
		return direct.P
	case errors.Is(err, idempotency.ErrKeyReused):
		return New(IdempotencyKeyReused, "Clave de idempotencia reutilizada", "La misma clave fue usada con un cuerpo distinto.")
	case errors.Is(err, idempotency.ErrRequestInProgress):
		return New(RequestInProgress, "Petición en curso", "Un request con la misma Idempotency-Key está en vuelo.")
	case errors.As(err, &de):
		p := New(Code(de.Code), titleFor(Code(de.Code)), de.Message)
		if _, known := defaultStatus[Code(de.Code)]; !known {
			p.Status = http.StatusConflict
		}
		return p
	case errors.As(err, &ve):
		p := New(ValidationFailed, "Datos inválidos", "Uno o más campos no son válidos.")
		for _, f := range ve.Fields {
			p.Errors = append(p.Errors, FieldError{Field: f.Field, Code: f.Code})
		}
		return p
	case errors.As(err, &nf):
		return New(NotFound, "No encontrado", "El recurso no existe o no es visible.")
	case errors.As(err, &fb):
		return New(Forbidden, "Prohibido", "No tienes permiso para esta acción.")
	case errors.As(err, &ce):
		return New(ConflictVersion, "Conflicto de versión", "El recurso cambió; recarga y reintenta.")
	case errors.Is(err, context.DeadlineExceeded):
		return New(Unavailable, "No disponible", "La operación tardó demasiado.")
	}
	return New(Internal, "Error interno", "Ocurrió un error inesperado.")
}

// #nosec G101 -- títulos del catálogo de errores, no credenciales.
//
//nolint:gosec // G101: idem.
var titles = map[Code]string{
	ConsentVersionOutdated:  "Consentimiento no vigente",
	AccountSuspended:        "Cuenta suspendida",
	OnboardingRequired:      "Onboarding pendiente",
	EmailNotVerified:        "Email no verificado",
	SameGuardian:            "Mismo tutor",
	TransferTokenInvalid:    "Enlace inválido o caducado",
	OrganizationNotEligible: "Organización no elegible",
	OrganizationSuspended:   "Organización suspendida",
	RoleNotAllowedForType:   "Rol no permitido",
	InvitationEmailMismatch: "Invitación para otro email",
	InvitationResendLimit:   "Límite de reenvíos",
	ReasonRequired:          "Motivo obligatorio",

	PetNotAccessible:             "Mascota no accesible",
	EncounterIncomplete:          "Consulta incompleta",
	NotAttendingVet:              "No es el profesional responsable",
	RoleRequiredVeterinarian:     "Requiere rol veterinario",
	AttendingVetNotEligible:      "Profesional no elegible",
	DiscardNotAllowed:            "Descarte no permitido",
	AttachmentNotFound:           "Adjunto no encontrado",
	Code("ENCOUNTER_NOT_DRAFT"):  "La consulta no está en borrador",
	Code("ENCOUNTER_NOT_SIGNED"): "La consulta no está firmada",
	Code("ATTACHMENT_LIMIT"):     "Límite de adjuntos",
	Code("ADDENDA_LIMIT"):        "Límite de adendas",
	Unavailable:                  "No disponible",

	Code("VOID_WINDOW_EXPIRED"):   "Ventana de anulación vencida",
	Code("VOID_NOT_ALLOWED"):      "Anulación no permitida",
	Code("GUARDIAN_REQUIRED"):     "Solo el tutor",
	Code("CARD_ENTRY_DUPLICATE"):  "Entrada duplicada",
	Code("CARD_ENTRIES_LIMIT"):    "Límite de entradas",
	Code("ENTRY_NOT_DECLARED"):    "Entrada no declarada",
	Code("ENTRY_NOT_ACTIVE"):      "Entrada anulada",
	Code("SHARE_LIMIT"):           "Límite de enlaces",
	Code("SHARE_ALREADY_CREATED"): "Enlace ya creado",
	Code("CATALOG_DUPLICATE"):     "Registro duplicado",
	Code("SHARE_EXPIRED"):         "Enlace caducado",

	CohortTooLarge:          "Cohorte demasiado amplia",
	CampaignRateLimited:     "Campaña limitada por frecuencia",
	DirectoryNotListed:      "Organización no listada en el directorio",
	ClaimContactMismatch:    "Contacto no coincide con la invitación",
	ClaimExpired:            "Invitación de reclamación caducada",
	ProvisionalOnlyCreator:  "Solo la clínica creadora puede gestionar el paciente provisional",
	PushSubscriptionInvalid: "Suscripción push inválida",
	AttachmentScanPending:   "Adjunto pendiente de análisis antivirus",
	AttachmentQuarantined:   "Adjunto en cuarentena por seguridad",
	Code("PIN_REQUIRED"):    "PIN requerido",
	Code("PIN_INVALID"):     "PIN incorrecto",
	Code("SHARE_LOCKED"):    "Enlace bloqueado",

	Code("SLOT_UNAVAILABLE"):     "Franja no disponible",
	Code("INVALID_TRANSITION"):   "Estado de la cita no válido",
	Code("OUTSIDE_LEAD_TIME"):    "Fuera de la antelación permitida",
	Code("REQUEST_LIMIT"):        "Límite de solicitudes",
	Code("PROPOSAL_EXPIRED"):     "Propuesta caducada",
	Code("CHECKIN_WINDOW"):       "Fuera de la ventana de check-in",
	Code("INVALID_PROFESSIONAL"): "Profesional no válido",

	Code("WEBHOOK_SIGNATURE_INVALID"): "Firma del webhook inválida",

	// Prescriptions / OCR F4 (PP-F4-API-010)
	Code("CREDENTIAL_REQUIRED"):       "Credencial requerida",
	Code("CREDENTIAL_EXPIRED"):        "Credencial caducada",
	Code("PRESCRIPTION_INCOMPLETE"):   "Receta incompleta",
	Code("VALIDITY_OUT_OF_RULES"):     "Vigencia fuera de reglas",
	Code("ITEM_INVALID"):              "Medicamento no válido",
	Code("ATTESTATION_REQUIRED"):      "Confirmación requerida",
	Code("PRESCRIPTION_NOT_DRAFT"):    "La receta no está en borrador",
	Code("PRESCRIPTION_DISPENSED"):    "Receta dispensada",
	Code("PRESCRIPTION_NOT_QUOTABLE"): "Receta no cotizable",
	Code("EXTRACTION_IN_PROGRESS"):    "Extracción en curso",
	Code("EXTRACTION_FAILED"):         "Error de extracción",
	Code("REUPLOAD_REQUIRED"):         "Imagen no legible",
	Code("EXTRACTION_RETRY_LIMIT"):    "Límite de reintentos de extracción superado",
	Code("AI_BUDGET_EXCEEDED"):        "Servicio de extracción temporalmente no disponible",
	Code("QUOTE_REQUEST_OPEN"):        "Hay una solicitud de cotización abierta",
	Code("EXTERNAL_UPLOAD_LIMIT"):     "Límite de imágenes superado",
	Code("EMPTY_IMAGES"):              "Imágenes no proporcionadas",

	// Billing (PP-F5-API-012, M5.1)
	PlanLimitReached:          "Límite del plan alcanzado",
	SubscriptionRestricted:    "Suscripción restringida",
	PaymentMethodRequired:     "Método de pago requerido",
	PaymentMethodInvalid:      "Método de pago inválido",
	SeatsBelowUsage:           "Asientos por debajo del uso",
	PlanNotAvailable:          "Plan no disponible",
	PlanChangePending:         "Cambio de plan pendiente",
	TaxIDInvalid:              "Identificación fiscal inválida",
	LegalEntityIncomplete:     "Entidad legal incompleta",
	OrganizationAlreadyBilled: "Organización ya facturada",
}

func titleFor(c Code) string {
	if t, ok := titles[c]; ok {
		return t
	}
	return "Operación no permitida"
}
