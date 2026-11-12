// Package problem implementa respuestas de error application/problem+json
// (RFC 9457) con el catálogo de códigos de docs/phase-1/05-api.md §5.
package problem

import (
	"encoding/json"
	"net/http"
)

// TypeBase es el prefijo de la URI de tipo de error.
const TypeBase = "https://docs.petpassport.app/errors/"

// Code es un código estable del catálogo.
type Code string

// Códigos del catálogo F1 (los de hitos posteriores se añaden cuando se usan).
const (
	ValidationFailed Code = "VALIDATION_FAILED"
	Unauthenticated  Code = "UNAUTHENTICATED"
	Forbidden        Code = "FORBIDDEN"
	NotFound         Code = "NOT_FOUND"
	Internal         Code = "INTERNAL"
	Unavailable      Code = "UNAVAILABLE"
	PayloadTooLarge  Code = "PAYLOAD_TOO_LARGE"
	UnsupportedMedia Code = "UNSUPPORTED_MEDIA_TYPE"

	EmailNotVerified       Code = "EMAIL_NOT_VERIFIED"
	OnboardingRequired     Code = "ONBOARDING_REQUIRED"
	ConsentVersionOutdated Code = "CONSENT_VERSION_OUTDATED"
	AccountSuspended       Code = "ACCOUNT_SUSPENDED"
	ConflictVersion        Code = "CONFLICT_VERSION"
	RequestInProgress      Code = "REQUEST_IN_PROGRESS"
	IdempotencyKeyReused   Code = "IDEMPOTENCY_KEY_REUSED"
	RateLimited            Code = "RATE_LIMITED"

	// Códigos de dominio con estado distinto de 409 (API-011, M1.3).
	SameGuardian            Code = "SAME_GUARDIAN"
	TransferTokenInvalid    Code = "TRANSFER_TOKEN_INVALID"
	TransferEmailMismatch   Code = "TRANSFER_EMAIL_MISMATCH"
	OrganizationNotEligible Code = "ORGANIZATION_NOT_ELIGIBLE"
	OrganizationSuspended   Code = "ORGANIZATION_SUSPENDED"
	RoleNotAllowedForType   Code = "ROLE_NOT_ALLOWED_FOR_TYPE"
	InvitationEmailMismatch Code = "INVITATION_EMAIL_MISMATCH"
	InvitationResendLimit   Code = "INVITATION_RESEND_LIMIT"
	ReasonRequired          Code = "REASON_REQUIRED"

	// Clinical Records (PP-F2-API-012, M2.1).
	PetNotAccessible         Code = "PET_NOT_ACCESSIBLE"
	EncounterIncomplete      Code = "ENCOUNTER_INCOMPLETE"
	NotAttendingVet          Code = "NOT_ATTENDING_VET"
	RoleRequiredVeterinarian Code = "ROLE_REQUIRED_VETERINARIAN"
	AttendingVetNotEligible  Code = "ATTENDING_VET_NOT_ELIGIBLE"
	DiscardNotAllowed        Code = "DISCARD_NOT_ALLOWED"
	AttachmentNotFound       Code = "ATTACHMENT_NOT_FOUND"

	// Search & Platform (PP-F3-API-011, M3.1).
	SearchUnavailable   Code = "SEARCH_UNAVAILABLE"
	SearchQueryInvalid  Code = "SEARCH_QUERY_INVALID"
	AdminReasonRequired Code = "ADMIN_REASON_REQUIRED"

	// Hardening & Features (PP-F3-API-011, M3.4)
	CohortTooLarge          Code = "COHORT_TOO_LARGE"
	CampaignRateLimited     Code = "CAMPAIGN_RATE_LIMITED"
	DirectoryNotListed      Code = "DIRECTORY_NOT_LISTED"
	ClaimContactMismatch    Code = "CLAIM_CONTACT_MISMATCH"
	ClaimExpired            Code = "CLAIM_EXPIRED"
	ProvisionalOnlyCreator  Code = "PROVISIONAL_ONLY_CREATOR"
	PushSubscriptionInvalid Code = "PUSH_SUBSCRIPTION_INVALID"
	AttachmentScanPending   Code = "ATTACHMENT_SCAN_PENDING"
	AttachmentQuarantined   Code = "ATTACHMENT_QUARANTINED"

	// Billing (PP-F5-API-012, M5.1)
	PlanLimitReached          Code = "PLAN_LIMIT_REACHED"
	SubscriptionRestricted    Code = "SUBSCRIPTION_RESTRICTED"
	PaymentMethodRequired     Code = "PAYMENT_METHOD_REQUIRED"
	PaymentMethodInvalid      Code = "PAYMENT_METHOD_INVALID"
	SeatsBelowUsage           Code = "SEATS_BELOW_USAGE"
	PlanNotAvailable          Code = "PLAN_NOT_AVAILABLE"
	PlanChangePending         Code = "PLAN_CHANGE_PENDING"
	TaxIDInvalid              Code = "TAX_ID_INVALID"
	LegalEntityIncomplete     Code = "LEGAL_ENTITY_INCOMPLETE"
	OrganizationAlreadyBilled Code = "ORGANIZATION_ALREADY_BILLED"
)

var defaultStatus = map[Code]int{
	ValidationFailed: http.StatusUnprocessableEntity,
	Unauthenticated:  http.StatusUnauthorized,
	Forbidden:        http.StatusForbidden,
	NotFound:         http.StatusNotFound,
	Internal:         http.StatusInternalServerError,
	Unavailable:      http.StatusServiceUnavailable,
	PayloadTooLarge:  http.StatusRequestEntityTooLarge,
	UnsupportedMedia: http.StatusUnsupportedMediaType,

	EmailNotVerified:       http.StatusForbidden,
	OnboardingRequired:     http.StatusForbidden,
	ConsentVersionOutdated: http.StatusUnprocessableEntity,
	AccountSuspended:       http.StatusForbidden,
	ConflictVersion:        http.StatusConflict,
	RequestInProgress:      http.StatusConflict,
	IdempotencyKeyReused:   http.StatusUnprocessableEntity,
	RateLimited:            http.StatusTooManyRequests,

	SameGuardian:            http.StatusUnprocessableEntity,
	TransferTokenInvalid:    http.StatusNotFound,
	TransferEmailMismatch:   http.StatusForbidden,
	OrganizationNotEligible: http.StatusUnprocessableEntity,
	OrganizationSuspended:   http.StatusForbidden,
	RoleNotAllowedForType:   http.StatusUnprocessableEntity,
	InvitationEmailMismatch: http.StatusForbidden,
	InvitationResendLimit:   http.StatusTooManyRequests,
	ReasonRequired:          http.StatusUnprocessableEntity,

	PetNotAccessible:         http.StatusForbidden,
	EncounterIncomplete:      http.StatusUnprocessableEntity,
	NotAttendingVet:          http.StatusForbidden,
	RoleRequiredVeterinarian: http.StatusForbidden,
	AttendingVetNotEligible:  http.StatusUnprocessableEntity,
	DiscardNotAllowed:        http.StatusForbidden,
	AttachmentNotFound:       http.StatusNotFound,

	SearchUnavailable:   http.StatusServiceUnavailable,
	SearchQueryInvalid:  http.StatusUnprocessableEntity,
	AdminReasonRequired: http.StatusUnprocessableEntity,

	CohortTooLarge:          http.StatusUnprocessableEntity,
	CampaignRateLimited:     http.StatusTooManyRequests,
	DirectoryNotListed:      http.StatusNotFound,
	ClaimContactMismatch:    http.StatusForbidden,
	ClaimExpired:            http.StatusGone,
	ProvisionalOnlyCreator:  http.StatusForbidden,
	PushSubscriptionInvalid: http.StatusUnprocessableEntity,
	AttachmentScanPending:   http.StatusConflict,
	AttachmentQuarantined:   http.StatusGone,

	PlanLimitReached:          http.StatusUnprocessableEntity,
	SubscriptionRestricted:    http.StatusPaymentRequired,
	PaymentMethodRequired:     http.StatusPaymentRequired,
	PaymentMethodInvalid:      http.StatusUnprocessableEntity,
	SeatsBelowUsage:           http.StatusUnprocessableEntity,
	PlanNotAvailable:          http.StatusUnprocessableEntity,
	PlanChangePending:         http.StatusConflict,
	TaxIDInvalid:              http.StatusUnprocessableEntity,
	LegalEntityIncomplete:     http.StatusUnprocessableEntity,
	OrganizationAlreadyBilled: http.StatusConflict,

	Code("VOID_WINDOW_EXPIRED"):   http.StatusForbidden,
	Code("VOID_NOT_ALLOWED"):      http.StatusForbidden,
	Code("GUARDIAN_REQUIRED"):     http.StatusForbidden,
	Code("CARD_ENTRY_DUPLICATE"):  http.StatusConflict,
	Code("CARD_ENTRIES_LIMIT"):    http.StatusConflict,
	Code("ENTRY_NOT_DECLARED"):    http.StatusConflict,
	Code("ENTRY_NOT_ACTIVE"):      http.StatusConflict,
	Code("SHARE_LIMIT"):           http.StatusConflict,
	Code("SHARE_ALREADY_CREATED"): http.StatusConflict,
	Code("CATALOG_DUPLICATE"):     http.StatusConflict,
	Code("SHARE_EXPIRED"):         http.StatusGone,
	Code("PIN_REQUIRED"):          http.StatusUnauthorized,
	Code("PIN_INVALID"):           http.StatusUnauthorized,
	Code("SHARE_LOCKED"):          http.StatusTooManyRequests,

	// Scheduling (M2.3); el resto (SLOT_UNAVAILABLE, INVALID_TRANSITION,
	// REQUEST_LIMIT, CHECKIN_WINDOW) usa el 409 por defecto.
	Code("OUTSIDE_LEAD_TIME"):    http.StatusUnprocessableEntity,
	Code("INVALID_PROFESSIONAL"): http.StatusUnprocessableEntity,
	Code("PROPOSAL_EXPIRED"):     http.StatusGone,

	// Notifications (M2.4): webhooks del proveedor de email.
	Code("WEBHOOK_SIGNATURE_INVALID"): http.StatusUnauthorized,

	// Billing: cobro y administración (M5.2). El resto usa el 409 por defecto.
	Code("PAYMENT_PROVIDER_UNAVAILABLE"): http.StatusServiceUnavailable,
	Code("REFUND_EXCEEDS_BALANCE"):       http.StatusUnprocessableEntity,
	Code("MANUAL_AMOUNT_MISMATCH"):       http.StatusUnprocessableEntity,
	Code("ADJUSTMENT_INVALID"):           http.StatusUnprocessableEntity,
	Code("COUPON_INVALID"):               http.StatusUnprocessableEntity,

	// Prescriptions / OCR F4 (PP-F4-API-010).
	Code("CREDENTIAL_REQUIRED"):       http.StatusForbidden,
	Code("CREDENTIAL_EXPIRED"):        http.StatusForbidden,
	Code("PRESCRIPTION_INCOMPLETE"):   http.StatusUnprocessableEntity,
	Code("VALIDITY_OUT_OF_RULES"):     http.StatusUnprocessableEntity,
	Code("ITEM_INVALID"):              http.StatusUnprocessableEntity,
	Code("ATTESTATION_REQUIRED"):      http.StatusUnprocessableEntity,
	Code("PRESCRIPTION_NOT_DRAFT"):    http.StatusConflict,
	Code("PRESCRIPTION_DISPENSED"):    http.StatusConflict,
	Code("PRESCRIPTION_NOT_QUOTABLE"): http.StatusUnprocessableEntity,
	Code("EXTRACTION_IN_PROGRESS"):    http.StatusConflict,
	Code("EXTRACTION_FAILED"):         http.StatusUnprocessableEntity,
	Code("REUPLOAD_REQUIRED"):         http.StatusUnprocessableEntity,
	Code("EXTRACTION_RETRY_LIMIT"):    http.StatusTooManyRequests,
	Code("AI_BUDGET_EXCEEDED"):        http.StatusServiceUnavailable,
	Code("QUOTE_REQUEST_OPEN"):        http.StatusConflict,
	Code("EXTERNAL_UPLOAD_LIMIT"):     http.StatusUnprocessableEntity,
	Code("EMPTY_IMAGES"):              http.StatusUnprocessableEntity,
}

// FieldError describe un error de validación por campo.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Problem es el cuerpo de la respuesta.
type Problem struct {
	Type          string       `json:"type"`
	Title         string       `json:"title"`
	Status        int          `json:"status"`
	Code          Code         `json:"code"`
	Detail        string       `json:"detail,omitempty"`
	Instance      string       `json:"instance,omitempty"`
	CorrelationID string       `json:"correlation_id,omitempty"`
	Errors        []FieldError `json:"errors,omitempty"`
}

// New construye un Problem con el estado por defecto del código.
func New(code Code, title, detail string) *Problem {
	status, ok := defaultStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	return &Problem{Type: TypeBase + string(code), Title: title, Status: status, Code: code, Detail: detail}
}

// Write serializa el Problem en la respuesta.
func Write(w http.ResponseWriter, r *http.Request, p *Problem) {
	if p.Instance == "" && r != nil {
		p.Instance = r.URL.Path
	}
	if p.CorrelationID == "" && r != nil {
		p.CorrelationID = r.Header.Get("X-Correlation-ID")
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
