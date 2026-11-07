package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CredentialStatus representa el estado de una tarjeta profesional (PP-F4-DOM-003).
type CredentialStatus string

const (
	CredentialStatusPending  CredentialStatus = "pending"
	CredentialStatusVerified CredentialStatus = "verified"
	CredentialStatusRejected CredentialStatus = "rejected"
	CredentialStatusExpired  CredentialStatus = "expired"
)

// Errores de dominio de credenciales profesionales.
var (
	ErrCredentialNotFound      = kernel.NewDomainError("CREDENTIAL_NOT_FOUND", "Credencial profesional no encontrada.")
	ErrCredentialInvalidState  = kernel.NewDomainError("CREDENTIAL_INVALID_STATE", "Operación no permitida en el estado actual de la credencial.")
	ErrCredentialCannotDelete  = kernel.NewDomainError("CREDENTIAL_CANNOT_DELETE", "Solo se pueden eliminar credenciales en estado pendiente o rechazado.")
	ErrCredentialExpired       = kernel.NewDomainError("CREDENTIAL_EXPIRED", "La credencial profesional ha caducado.")
	ErrCredentialAlreadyActive = kernel.NewDomainError("CREDENTIAL_ALREADY_ACTIVE", "El usuario ya tiene una credencial verificada o pendiente para este país.")
)

// ProfessionalCredential es la credencial profesional del veterinario (PP-F4-DOM-003).
// Vive en Identity & Access como CRUD auditado (RN-22, ADR-018).
type ProfessionalCredential struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Country           string
	Kind              string
	RegistryNumber    string
	Issuer            string
	IssuedAt          time.Time
	ExpiresAt         *time.Time
	EvidenceObjectKey string
	Status            CredentialStatus
	ReviewedBy        *uuid.UUID
	Reason            *string
	ReviewedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewProfessionalCredential crea una nueva credencial en estado pending.
func NewProfessionalCredential(
	id uuid.UUID,
	userID uuid.UUID,
	country string,
	kind string,
	registryNumber string,
	issuer string,
	issuedAt time.Time,
	expiresAt *time.Time,
	evidenceObjectKey string,
	now time.Time,
) (*ProfessionalCredential, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		return nil, kernel.NewDomainError("INVALID_COUNTRY", "El código de país debe tener 2 letras.")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "veterinarian"
	}
	if kind != "veterinarian" {
		return nil, kernel.NewDomainError("INVALID_KIND", "El tipo de credencial debe ser veterinarian.")
	}
	registryNumber = strings.TrimSpace(registryNumber)
	if len(registryNumber) < 3 {
		return nil, kernel.NewDomainError("INVALID_REGISTRY_NUMBER", "El número de tarjeta o registro profesional debe tener al menos 3 caracteres.")
	}
	issuer = strings.TrimSpace(issuer)
	if len(issuer) < 2 {
		return nil, kernel.NewDomainError("INVALID_ISSUER", "La entidad emisora debe tener al menos 2 caracteres.")
	}
	evidenceObjectKey = strings.TrimSpace(evidenceObjectKey)
	if evidenceObjectKey == "" {
		return nil, kernel.NewDomainError("INVALID_EVIDENCE", "El documento adjunto de evidencia es obligatorio.")
	}
	if expiresAt != nil && !expiresAt.After(issuedAt) {
		return nil, kernel.NewDomainError("INVALID_EXPIRATION", "La fecha de caducidad debe ser posterior a la de emisión.")
	}

	return &ProfessionalCredential{
		ID:                id,
		UserID:            userID,
		Country:           country,
		Kind:              kind,
		RegistryNumber:    registryNumber,
		Issuer:            issuer,
		IssuedAt:          issuedAt,
		ExpiresAt:         expiresAt,
		EvidenceObjectKey: evidenceObjectKey,
		Status:            CredentialStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

// Verify transiciona la credencial a estado verified tras revisión humana de SuperAdmin.
func (c *ProfessionalCredential) Verify(reviewerID uuid.UUID, reason string, at time.Time) error {
	if c.Status != CredentialStatusPending && c.Status != CredentialStatusRejected {
		return ErrCredentialInvalidState
	}
	c.Status = CredentialStatusVerified
	c.ReviewedBy = &reviewerID
	trimmed := strings.TrimSpace(reason)
	if trimmed != "" {
		c.Reason = &trimmed
	} else {
		c.Reason = nil
	}
	c.ReviewedAt = &at
	c.UpdatedAt = at
	return nil
}

// Reject rechaza la credencial con un motivo obligatorio.
func (c *ProfessionalCredential) Reject(reviewerID uuid.UUID, reason string, at time.Time) error {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return kernel.NewDomainError("REASON_REQUIRED", "El motivo de rechazo es obligatorio.")
	}
	if c.Status != CredentialStatusPending && c.Status != CredentialStatusVerified {
		return ErrCredentialInvalidState
	}
	c.Status = CredentialStatusRejected
	c.ReviewedBy = &reviewerID
	c.Reason = &trimmed
	c.ReviewedAt = &at
	c.UpdatedAt = at
	return nil
}

// CanBeDeleted comprueba si el usuario puede eliminar esta credencial (solo pending o rejected).
func (c *ProfessionalCredential) CanBeDeleted() bool {
	return c.Status == CredentialStatusPending || c.Status == CredentialStatusRejected
}

// IsActive verifica si la credencial está activa y vigente a una fecha dada.
func (c *ProfessionalCredential) IsActive(now time.Time) bool {
	if c.Status != CredentialStatusVerified {
		return false
	}
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return false
	}
	return true
}
