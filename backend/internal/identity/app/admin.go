package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// KeycloakAdmin es el ACL hacia la Admin REST API de Keycloak con la service
// account petpassport-backend (IAM-011, DOM-010). Sin permisos para crear
// usuarios ni roles.
type KeycloakAdmin interface {
	SetEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error
	LogoutAllSessions(ctx context.Context, userID uuid.UUID) error
	SendVerifyEmail(ctx context.Context, userID uuid.UUID) error
}

// ErrKeycloakUserNotFound lo devuelve KeycloakAdmin si el usuario no existe en
// Keycloak (no hay nada que deshabilitar: la suspensión en la plataforma basta).
var ErrKeycloakUserNotFound = errors.New("keycloak: user not found")

// ErrIdentityProviderUnavailable: Keycloak no respondió; la cuenta ya quedó
// suspendida/reactivada en la plataforma y reintentar es seguro (503).
var ErrIdentityProviderUnavailable = kernel.NewDomainError("UNAVAILABLE",
	"El proveedor de identidad no respondió; la acción quedó registrada en la plataforma, reintenta para completarla.")

// AccountCursor es la posición (created_at, id) del último usuario devuelto.
type AccountCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// AccountSearch filtra la búsqueda admin (PRD-018).
type AccountSearch struct {
	Query  string // subcadena de email o nombre, sin distinguir mayúsculas
	Status string // "" | active | suspended
	Cursor *AccountCursor
	Limit  int
}

// AccountDirectory es la búsqueda de cuentas para el panel admin.
type AccountDirectory interface {
	SearchAccounts(ctx context.Context, f AccountSearch) ([]*domain.UserAccount, *AccountCursor, error)
}

// PetCounter es el puerto hacia Pet Registry: mascotas por tutor (AdminUserRow.pet_count).
type PetCounter interface {
	CountByGuardians(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error)
}

// AuditLog es la lectura de platform.audit_log (GET /admin/audit-log).
type AuditLog interface {
	List(ctx context.Context, f audit.Filter) ([]audit.Row, *audit.Cursor, error)
}

// AdminOptions construye el AdminService. Keycloak nil = sincronización con
// Keycloak deshabilitada (solo dev sin secreto de la service account).
type AdminOptions struct {
	Repo        Repository
	Directory   AccountDirectory
	Keycloak    KeycloakAdmin
	Pets        PetCounter
	Memberships auth.MembershipLoader
	Audit       AuditLog
	Logger      *slog.Logger
	Now         func() time.Time
}

// AdminService implementa los casos de uso de SuperAdmin sobre cuentas
// (PRD-018, API-008). Cada uno evalúa authz.Can; los comandos exigen motivo
// y escriben audit_log en la misma transacción que el cambio (RN-06).
type AdminService struct {
	opts AdminOptions
}

// NewAdminService construye el servicio.
func NewAdminService(opts AdminOptions) *AdminService {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &AdminService{opts: opts}
}

// AdminUserView es una fila de AdminUserRow.
type AdminUserView struct {
	Account     *domain.UserAccount
	PetCount    int
	Memberships []auth.Membership
}

// SearchUsers busca usuarios por email/nombre (admin:user:list).
func (s *AdminService) SearchUsers(ctx context.Context, p auth.Principal, f AccountSearch) ([]AdminUserView, *AccountCursor, error) {
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	f.Query = strings.TrimSpace(f.Query)
	accounts, next, err := s.opts.Directory.SearchAccounts(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	views, err := s.views(ctx, accounts)
	if err != nil {
		return nil, nil, err
	}
	return views, next, nil
}

// AdminUserCommand es POST /admin/users/{id}/suspend|reactivate.
type AdminUserCommand struct {
	UserID        uuid.UUID
	Reason        string
	CorrelationID *uuid.UUID
	IPHash        []byte
}

// SuspendUser marca la cuenta suspended (la API responde 403
// ACCOUNT_SUSPENDED desde el siguiente request), y después deshabilita el
// usuario en Keycloak y revoca sus sesiones (PRD-018). El orden prioriza
// cortar el acceso a la plataforma aunque Keycloak falle; reintentar es
// idempotente.
func (s *AdminService) SuspendUser(ctx context.Context, p auth.Principal, cmd AdminUserCommand) (AdminUserView, error) {
	if d := authz.Can(p, authz.AdminUserSuspend, nil); !d.Allow {
		return AdminUserView{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	if cmd.UserID == p.UserID {
		return AdminUserView{}, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "user_id", Code: "SELF_SUSPENSION"}}}
	}
	return s.transition(ctx, p, cmd, "admin.user.suspend", func(a *domain.UserAccount, now time.Time) (bool, error) {
		return a.Suspend(cmd.Reason, now)
	}, func(ctx context.Context, kc KeycloakAdmin) error {
		if err := kc.SetEnabled(ctx, cmd.UserID, false); err != nil {
			return err
		}
		return kc.LogoutAllSessions(ctx, cmd.UserID)
	})
}

// ReactivateUser reactiva la cuenta y la habilita en Keycloak.
func (s *AdminService) ReactivateUser(ctx context.Context, p auth.Principal, cmd AdminUserCommand) (AdminUserView, error) {
	if d := authz.Can(p, authz.AdminUserReactivate, nil); !d.Allow {
		return AdminUserView{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	return s.transition(ctx, p, cmd, "admin.user.reactivate", func(a *domain.UserAccount, now time.Time) (bool, error) {
		return a.Reactivate(cmd.Reason, now)
	}, func(ctx context.Context, kc KeycloakAdmin) error {
		return kc.SetEnabled(ctx, cmd.UserID, true)
	})
}

func (s *AdminService) transition(ctx context.Context, p auth.Principal, cmd AdminUserCommand, action string,
	apply func(*domain.UserAccount, time.Time) (bool, error), sync func(context.Context, KeycloakAdmin) error,
) (AdminUserView, error) {
	if _, err := domain.NormalizeReason(cmd.Reason); err != nil {
		return AdminUserView{}, err
	}
	acc, err := s.opts.Repo.Get(ctx, cmd.UserID)
	if err != nil {
		return AdminUserView{}, err
	}
	changed, err := apply(acc, s.opts.Now())
	if err != nil {
		return AdminUserView{}, err
	}
	actor, target := p.UserID, acc.ID
	entry := &audit.Entry{
		ActorID: &actor, ActorRoles: p.GlobalRoles, Action: action,
		TargetType: "user_account", TargetID: &target, Reason: acc.StatusReason,
		Details:       map[string]any{"status": string(acc.Status), "status_changed": changed},
		CorrelationID: cmd.CorrelationID, IPHash: cmd.IPHash,
	}
	if err := s.opts.Repo.Save(ctx, acc, entry); err != nil {
		return AdminUserView{}, err
	}
	if s.opts.Keycloak == nil {
		s.opts.Logger.WarnContext(ctx, "keycloak admin deshabilitado: estado solo en la plataforma", "action", action, "target_id", target.String())
	} else if err := sync(context.WithoutCancel(ctx), s.opts.Keycloak); err != nil && !errors.Is(err, ErrKeycloakUserNotFound) {
		s.opts.Logger.ErrorContext(ctx, "keycloak admin falló", "action", action, "target_id", target.String(), "error", err.Error())
		return AdminUserView{}, ErrIdentityProviderUnavailable
	}
	views, err := s.views(ctx, []*domain.UserAccount{acc})
	if err != nil {
		return AdminUserView{}, err
	}
	return views[0], nil
}

func (s *AdminService) views(ctx context.Context, accounts []*domain.UserAccount) ([]AdminUserView, error) {
	ids := make([]uuid.UUID, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}
	counts := map[uuid.UUID]int{}
	if s.opts.Pets != nil && len(ids) > 0 {
		c, err := s.opts.Pets.CountByGuardians(ctx, ids)
		if err != nil {
			return nil, err
		}
		counts = c
	}
	out := make([]AdminUserView, len(accounts))
	for i, a := range accounts {
		v := AdminUserView{Account: a, PetCount: counts[a.ID], Memberships: []auth.Membership{}}
		if s.opts.Memberships != nil {
			ms, err := s.opts.Memberships.Memberships(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			if ms != nil {
				v.Memberships = ms
			}
		}
		out[i] = v
	}
	return out, nil
}

// AuditEntries lista platform.audit_log (admin:audit:read).
func (s *AdminService) AuditEntries(ctx context.Context, p auth.Principal, f audit.Filter) ([]audit.Row, *audit.Cursor, error) {
	if d := authz.Can(p, authz.AdminAuditRead, nil); !d.Allow {
		return nil, nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return nil, nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "to", Code: "BEFORE_FROM"}}}
	}
	return s.opts.Audit.List(ctx, f)
}
