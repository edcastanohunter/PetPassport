// Package app expone los casos de uso de Partner Network (ARQ-003): registro
// y edición de organizaciones, verificación, miembros
// e invitaciones, y las transiciones de SuperAdmin (M1.4). Cada comando: authz (en http) → idempotency (en http) →
// uow.Do {load → reserve → handle → append → project(sync)} → tras el commit:
// invalidación de membresías y emails best-effort (DOM-011).
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
)

// Repository persiste y rehidrata el agregado Organization.
type Repository interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.Organization, error)
	Save(ctx context.Context, o *domain.Organization, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
}

// TxRunner ejecuta una función dentro de la transacción del UoW.
type TxRunner interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica los proyectores síncronos dentro de la misma tx.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// PublicCodeReservations gestiona la unicidad del código público (O5, DAT-006).
type PublicCodeReservations interface {
	// Reserve inserta la reserva en la tx del comando; ErrPublicCodeTaken si
	// el código ya es de otra organización.
	Reserve(ctx context.Context, code string, orgID uuid.UUID) error
}

// InvitationTokens es el almacén operativo de tokens de invitación: el hash
// vive en read_partnernetwork.invitations (no en eventos, decisión M1.2).
type InvitationTokens interface {
	// Rotate sustituye el token de una invitación pendiente y vigente de la org
	// y cuenta el reenvío (máx domain.MaxResends24h en 24 h, API-007).
	// Devuelve email y rol para el correo. ErrResendLimit / *kernel.NotFoundError.
	Rotate(ctx context.Context, orgID, invitationID uuid.UUID, hash []byte, now time.Time) (email, role string, err error)
}

// UserLookup es el puerto hacia Identity (DOM-010).
type UserLookup interface {
	// FindByEmail devuelve el usuario con ese email (normalizado), si existe.
	FindByEmail(ctx context.Context, email string) (uuid.UUID, bool)
	// Info devuelve email y nombre para mostrar; ok=false si no existe.
	Info(ctx context.Context, id uuid.UUID) (UserInfo, bool)
}

// UserInfo es lo que Partner Network sabe de un usuario.
type UserInfo struct {
	Email       string
	DisplayName string
}

// MembershipInvalidator invalida la caché de membresías del Principal
// (IAM-008) cuando cambian membresías o el estado de la org (DOM-011).
type MembershipInvalidator interface {
	InvalidateUsers(ids []uuid.UUID)
}

// Mailer es el puerto de email (DOM-011); el adaptador es platform/mail.SMTP.
// Los envíos son best-effort tras el commit: un fallo se registra y NO
// revierte el comando. En F2 migran al process manager (ADR-012).
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// ErrPublicCodeTaken: el código público lo tiene otra organización (O5).
var ErrPublicCodeTaken = errors.New("public code taken")

// ErrResendLimit: más de domain.MaxResends24h reenvíos en 24 h (429).
var ErrResendLimit = errors.New("invitation resend limit")

// ErrOrgNotEligible: la organización no es clínica activa (I5).
var ErrOrgNotEligible = errors.New("organization not eligible")

// ----------------------------------------------------------------- consultas

// OrgView es la organización proyectada completa (GET /organizations/{id}).
type OrgView struct {
	ID           uuid.UUID
	Version      int64
	Type         string
	LegalName    string
	DisplayName  string
	TaxID        domain.TaxIDPayload
	Address      domain.AddressPayload
	Contact      domain.ContactPayload
	City         string
	Country      string
	PublicCode   string
	Status       string
	StatusReason string
	FounderID    uuid.UUID
	MemberCount  int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MemberView es un miembro activo proyectado (email/nombre vía UserLookup).
type MemberView struct {
	UserID      uuid.UUID
	Email       string
	DisplayName string
	Role        string
	Status      string
	JoinedAt    time.Time
}

// InvitationView es una invitación proyectada. Status "expired" se deriva de
// expires_at en la consulta (no hay evento de caducidad).
type InvitationView struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Email          string
	Role           string
	Status         string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

// OrgSummary es el resumen público (API-005 lookup; API-006 para no miembros).
type OrgSummary struct {
	ID          uuid.UUID
	Type        string
	DisplayName string
	City        string
	Country     string
	Status      string
}

// MembershipRow es una membresía activa de un usuario (Principal, IAM-008).
type MembershipRow struct {
	OrganizationID uuid.UUID
	DisplayName    string
	OrgType        string
	Role           string
	OrgStatus      string
}

// Queries es el lado de consulta del módulo (ARQ-009).
type Queries interface {
	GetOrg(ctx context.Context, id uuid.UUID) (OrgView, error)
	ListMembers(ctx context.Context, orgID uuid.UUID) ([]MemberView, error)
	ListInvitations(ctx context.Context, orgID uuid.UUID, status string, now time.Time) ([]InvitationView, error)
	// InvitationByTokenHash resuelve el token; *kernel.NotFoundError si no existe.
	InvitationByTokenHash(ctx context.Context, hash []byte, now time.Time) (InvitationView, error)
	// SummaryByCode: solo clínicas activas; otra cosa → *kernel.NotFoundError.
	SummaryByCode(ctx context.Context, code string) (OrgSummary, error)
	// ActiveClinic: ErrOrgNotEligible si no es clínica activa (puerto de Pet Registry).
	ActiveClinic(ctx context.Context, orgID uuid.UUID) (OrgSummary, error)
	MembershipsOf(ctx context.Context, userID uuid.UUID) ([]MembershipRow, error)
	// AdminListOrgs: panel admin por estado, FIFO por (created_at, id) (API-008).
	AdminListOrgs(ctx context.Context, f AdminOrgFilter) ([]OrgView, *OrgCursor, error)
}

// AdminOrgFilter filtra la cola admin (PRD-017).
type AdminOrgFilter struct {
	Status string // pending_verification por defecto
	Query  string
	Cursor *OrgCursor
	Limit  int
}

// OrgCursor es la posición (created_at, id) de la última organización devuelta.
type OrgCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Auditor escribe en platform.audit_log dentro de la transacción del
// comando (RN-06: toda acción de SuperAdmin queda auditada con motivo).
type Auditor interface {
	Record(ctx context.Context, e audit.Entry) error
}

// ------------------------------------------------- hash de token en contexto

type tokenHashKey struct{}

// WithInvitationTokenHash adjunta el hash del token de la invitación que el
// comando va a emitir; el proyector lo lee en la misma tx (DAT-006).
func WithInvitationTokenHash(ctx context.Context, invitationID uuid.UUID, hash []byte) context.Context {
	return context.WithValue(ctx, tokenHashKey{}, map[uuid.UUID][]byte{invitationID: hash})
}

// InvitationTokenHashFrom devuelve el hash adjunto; nil en replay/rebuild (el
// proyector genera uno aleatorio: los tokens vivos se invalidan y la
// invitación debe reenviarse).
func InvitationTokenHashFrom(ctx context.Context, invitationID uuid.UUID) []byte {
	m, _ := ctx.Value(tokenHashKey{}).(map[uuid.UUID][]byte)
	return m[invitationID]
}
