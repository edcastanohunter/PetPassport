package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// MemberCredentialSummary resume el estado de la credencial de un miembro para la clínica (PP-F4-API-001).
// No incluye el documento adjunto (privacidad).
type MemberCredentialSummary struct {
	UserID         uuid.UUID  `json:"user_id"`
	Country        string     `json:"country"`
	Kind           string     `json:"kind"`
	RegistryNumber string     `json:"registry_number"`
	Issuer         string     `json:"issuer"`
	Status         string     `json:"status"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

// CredentialRef representa la referencia inmutable capturada al emitir una receta (PP-F4-DOM-005).
type CredentialRef struct {
	Country        string    `json:"country"`
	RegistryNumber string    `json:"registry_number"`
	VerifiedAt     time.Time `json:"verified_at"`
}

// CredentialStore es el puerto de persistencia de credenciales profesionales (identity.professional_credentials).
type CredentialStore interface {
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.ProfessionalCredential, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.ProfessionalCredential, error)
	GetVerifiedByUserAndCountry(ctx context.Context, userID uuid.UUID, country string) (*domain.ProfessionalCredential, error)
	Create(ctx context.Context, cred *domain.ProfessionalCredential) error
	Update(ctx context.Context, cred *domain.ProfessionalCredential) error
	Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	ListByOrgMembers(ctx context.Context, memberUserIDs []uuid.UUID) ([]MemberCredentialSummary, error)
	ListByOrg(ctx context.Context, orgID uuid.UUID) ([]MemberCredentialSummary, error)
	ListByStatus(ctx context.Context, status string) ([]domain.ProfessionalCredential, error)
}

// SubmitCredentialCmd contiene los campos enviados por el veterinario para registrar su credencial.
type SubmitCredentialCmd struct {
	Country           string
	Kind              string
	RegistryNumber    string
	Issuer            string
	IssuedAt          time.Time
	ExpiresAt         *time.Time
	EvidenceObjectKey string
}

// CredentialService gestiona el ciclo de vida de credenciales profesionales y expone el puerto CredentialCheck.
type CredentialService struct {
	store  CredentialStore
	logger *slog.Logger
	now    func() time.Time
}

// NewCredentialService construye el servicio.
func NewCredentialService(store CredentialStore, logger *slog.Logger) *CredentialService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CredentialService{
		store:  store,
		logger: logger,
		now:    time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (s *CredentialService) WithClock(now func() time.Time) *CredentialService {
	s.now = now
	return s
}

// ListMyCredentials devuelve las credenciales registradas por el usuario actual (GET /me/credentials).
func (s *CredentialService) ListMyCredentials(ctx context.Context, p auth.Principal) ([]domain.ProfessionalCredential, error) {
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}
	return s.store.ListByUserID(ctx, p.UserID)
}

// SubmitCredential registra una nueva tarjeta profesional (POST /me/credentials).
func (s *CredentialService) SubmitCredential(ctx context.Context, p auth.Principal, cmd SubmitCredentialCmd) (*domain.ProfessionalCredential, error) {
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}

	id := kernel.NewID()
	cred, err := domain.NewProfessionalCredential(
		id,
		p.UserID,
		cmd.Country,
		cmd.Kind,
		cmd.RegistryNumber,
		cmd.Issuer,
		cmd.IssuedAt,
		cmd.ExpiresAt,
		cmd.EvidenceObjectKey,
		s.now(),
	)
	if err != nil {
		return nil, err
	}

	if err := s.store.Create(ctx, cred); err != nil {
		return nil, fmt.Errorf("credential store: %w", err)
	}

	s.logger.Info("credential submitted", "credential_id", cred.ID, "user_id", cred.UserID, "country", cred.Country)
	return cred, nil
}

// DeleteCredential elimina una credencial propia (DELETE /me/credentials/{id}), solo si está pending o rejected.
func (s *CredentialService) DeleteCredential(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return kernel.NewDomainError("FORBIDDEN", d.Reason)
	}

	cred, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if cred.UserID != p.UserID {
		return domain.ErrCredentialNotFound
	}
	if !cred.CanBeDeleted() {
		return domain.ErrCredentialCannotDelete
	}

	return s.store.Delete(ctx, id, p.UserID)
}

// ListOrgMembersCredentials permite a miembros de una clínica ver el estado de credenciales de sus veterinarios.
func (s *CredentialService) ListOrgMembersCredentials(ctx context.Context, p auth.Principal, orgID uuid.UUID, memberUserIDs []uuid.UUID) ([]MemberCredentialSummary, error) {
	if d := authz.Can(p, authz.OrgRead, authz.OrgResource{ID: orgID, MembersOnly: true}); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}
	if len(memberUserIDs) == 0 {
		return []MemberCredentialSummary{}, nil
	}
	return s.store.ListByOrgMembers(ctx, memberUserIDs)
}

// ListByOrg lista las credenciales de los miembros de una organización consultando la vista de miembros.
func (s *CredentialService) ListByOrg(ctx context.Context, p auth.Principal, orgID uuid.UUID) ([]MemberCredentialSummary, error) {
	if d := authz.Can(p, authz.OrgRead, authz.OrgResource{ID: orgID, MembersOnly: true}); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}
	return s.store.ListByOrg(ctx, orgID)
}

// AdminListCredentials lista credenciales para revisión de SuperAdmin.
func (s *CredentialService) AdminListCredentials(ctx context.Context, p auth.Principal, status string) ([]domain.ProfessionalCredential, error) {
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}
	return s.store.ListByStatus(ctx, status)
}

// AdminVerifyCredential aprueba una credencial tras verificación humana (POST /admin/credentials/{id}/verify).
func (s *CredentialService) AdminVerifyCredential(ctx context.Context, p auth.Principal, id uuid.UUID, reason string) (*domain.ProfessionalCredential, error) {
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}

	cred, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := cred.Verify(p.UserID, reason, s.now()); err != nil {
		return nil, err
	}

	if err := s.store.Update(ctx, cred); err != nil {
		return nil, err
	}

	s.logger.Info("credential verified by admin", "credential_id", id, "admin_id", p.UserID)
	return cred, nil
}

// AdminRejectCredential rechaza una credencial (POST /admin/credentials/{id}/reject).
func (s *CredentialService) AdminRejectCredential(ctx context.Context, p auth.Principal, id uuid.UUID, reason string) (*domain.ProfessionalCredential, error) {
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, kernel.NewDomainError("FORBIDDEN", d.Reason)
	}

	cred, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := cred.Reject(p.UserID, reason, s.now()); err != nil {
		return nil, err
	}

	if err := s.store.Update(ctx, cred); err != nil {
		return nil, err
	}

	s.logger.Info("credential rejected by admin", "credential_id", id, "admin_id", p.UserID, "reason", reason)
	return cred, nil
}

// VerifiedCredential implementa el puerto CredentialCheck para el contexto de prescripciones (PP-F4-DOM-003, PP-F4-DOM-013).
// Devuelve la referencia inmutable y true si el veterinario cuenta con credencial verificada y vigente en el país dado.
func (s *CredentialService) VerifiedCredential(ctx context.Context, userID uuid.UUID, country string) (*CredentialRef, bool, error) {
	cred, err := s.store.GetVerifiedByUserAndCountry(ctx, userID, country)
	if err != nil {
		var nf *kernel.NotFoundError
		if errors.As(err, &nf) || errors.Is(err, domain.ErrCredentialNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	now := s.now()
	if !cred.IsActive(now) {
		return nil, false, nil
	}

	verifiedAt := cred.CreatedAt
	if cred.ReviewedAt != nil {
		verifiedAt = *cred.ReviewedAt
	}

	return &CredentialRef{
		Country:        cred.Country,
		RegistryNumber: cred.RegistryNumber,
		VerifiedAt:     verifiedAt,
	}, true, nil
}
