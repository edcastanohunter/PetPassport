// Package app expone los casos de uso de Identity & Access: aprovisionamiento JIT
// (IAM-009), onboarding (PRD-003), consentimiento (PRD-020) y perfil (PRD-004).
// Define los puertos; los adaptadores viven en infra. Cada caso de uso evalúa
// authz.Can antes de actuar.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Repository es el puerto de persistencia de UserAccount.
type Repository interface {
	// Get devuelve la cuenta o *kernel.NotFoundError.
	Get(ctx context.Context, id uuid.UUID) (*domain.UserAccount, error)
	// Insert crea la cuenta (JIT). Si ya existe (carrera), devuelve ErrAlreadyExists.
	Insert(ctx context.Context, a *domain.UserAccount) error
	// Save persiste el estado completo y, si entry != nil, la entrada de auditoría en la misma transacción.
	Save(ctx context.Context, a *domain.UserAccount, entry *audit.Entry) error
	// CurrentConsent devuelve las versiones vigentes.
	CurrentConsent(ctx context.Context) (domain.ConsentVersions, error)
}

// ErrAlreadyExists lo devuelve Insert ante una carrera de aprovisionamiento.
var ErrAlreadyExists = errors.New("account already exists")

// LoginThrottle es el intervalo mínimo entre escrituras de last_login_at por usuario (IAM-009).
const LoginThrottle = 5 * time.Minute

// consentTTL es la caché en memoria de las versiones vigentes.
const consentTTL = 60 * time.Second

// Service implementa los casos de uso y el puerto auth.AccountResolver.
type Service struct {
	repo   Repository
	logger *slog.Logger
	now    func() time.Time

	mu        sync.Mutex
	lastLogin map[uuid.UUID]time.Time

	cmu        sync.Mutex
	consent    domain.ConsentVersions
	consentExp time.Time
}

// NewService construye el servicio.
func NewService(repo Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, logger: logger, now: time.Now, lastLogin: map[uuid.UUID]time.Time{}}
}

// WithClock fija el reloj (pruebas).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

var _ auth.AccountResolver = (*Service)(nil)

// Resolve es EnsureUserAccount (IAM-009): upsert barato por request autenticado
// con throttle de 5 min para last_login_at. Devuelve el estado que el guard
// necesita para decidir.
func (s *Service) Resolve(ctx context.Context, c auth.Claims) (auth.AccountState, error) {
	id, err := uuid.Parse(c.Subject)
	if err != nil {
		return auth.AccountState{}, fmt.Errorf("sub inválido: %w", err)
	}
	snap, err := snapshotFromClaims(c)
	if err != nil {
		return auth.AccountState{}, err
	}
	now := s.now()
	acc, err := s.repo.Get(ctx, id)
	var nf *kernel.NotFoundError
	switch {
	case errors.As(err, &nf):
		acc = domain.Provision(id, snap, now)
		if err := s.repo.Insert(ctx, acc); err != nil {
			if !errors.Is(err, ErrAlreadyExists) {
				return auth.AccountState{}, err
			}
			// Carrera: otro request la creó; releer.
			if acc, err = s.repo.Get(ctx, id); err != nil {
				return auth.AccountState{}, err
			}
		} else {
			s.markLogin(id, now)
			s.logger.InfoContext(ctx, "account provisioned", "actor_id", id.String())
		}
	case err != nil:
		return auth.AccountState{}, err
	}
	changed := acc.SyncFromToken(snap, now)
	if s.shouldTouchLogin(id, now) {
		acc.TouchLogin(now)
		changed = true
	}
	if changed {
		if err := s.repo.Save(ctx, acc, nil); err != nil {
			return auth.AccountState{}, err
		}
		s.markLogin(id, now)
	}
	cur, err := s.currentConsent(ctx)
	if err != nil {
		return auth.AccountState{}, err
	}
	return auth.AccountState{
		Suspended: acc.IsSuspended(), OnboardingCompleted: acc.IsOnboarded(), ConsentCurrent: acc.ConsentCurrent(cur),
	}, nil
}

func (s *Service) shouldTouchLogin(id uuid.UUID, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastLogin[id]
	return !ok || now.Sub(last) >= LoginThrottle
}

func (s *Service) markLogin(id uuid.UUID, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lastLogin) > 100_000 { // cota de memoria; se reconstruye sola
		s.lastLogin = map[uuid.UUID]time.Time{}
	}
	s.lastLogin[id] = now
}

func (s *Service) currentConsent(ctx context.Context) (domain.ConsentVersions, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	now := s.now()
	if now.Before(s.consentExp) {
		return s.consent, nil
	}
	cur, err := s.repo.CurrentConsent(ctx)
	if err != nil {
		return domain.ConsentVersions{}, err
	}
	s.consent, s.consentExp = cur, now.Add(consentTTL)
	return cur, nil
}

// InvalidateConsentCache fuerza releer las versiones vigentes (admin/pruebas).
func (s *Service) InvalidateConsentCache() {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	s.consentExp = time.Time{}
}

func snapshotFromClaims(c auth.Claims) (domain.TokenSnapshot, error) {
	snap := domain.TokenSnapshot{EmailVerified: c.EmailVerified, DisplayName: c.DisplayName(), Roles: c.Roles()}
	if c.Email != "" {
		email, err := kernel.NewEmail(c.Email)
		if err != nil {
			return snap, fmt.Errorf("email del token: %w", err)
		}
		snap.Email = email
	}
	if l, err := kernel.NewLocale(c.Locale); err == nil {
		snap.Locale = l
	}
	return snap, nil
}

// MeView es la vista de cuenta propia que devuelven los casos de uso.
type MeView struct {
	Account *domain.UserAccount
	Current domain.ConsentVersions
}

// GetMe devuelve la cuenta del actor (me:read).
func (s *Service) GetMe(ctx context.Context, p auth.Principal) (MeView, error) {
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return MeView{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	acc, err := s.repo.Get(ctx, p.UserID)
	if err != nil {
		return MeView{}, err
	}
	cur, err := s.currentConsent(ctx)
	if err != nil {
		return MeView{}, err
	}
	return MeView{Account: acc, Current: cur}, nil
}

// LegalCurrent devuelve las versiones vigentes (público).
func (s *Service) LegalCurrent(ctx context.Context) (domain.ConsentVersions, error) {
	return s.currentConsent(ctx)
}

// CompleteOnboardingCommand son los datos de POST /me/onboarding ya validados
// por el esquema; aquí se normalizan a VOs.
type CompleteOnboardingCommand struct {
	DisplayName    string
	Country        string
	Locale         string
	Phone          *string
	TermsVersion   string
	PrivacyVersion string
	CorrelationID  *uuid.UUID
	IPHash         []byte
}

// CompleteOnboarding ejecuta onboarding:complete.
func (s *Service) CompleteOnboarding(ctx context.Context, p auth.Principal, cmd CompleteOnboardingCommand) (MeView, error) {
	if d := authz.Can(p, authz.OnboardingComplete, nil); !d.Allow {
		return MeView{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	var ve kernel.ValidationError
	data := domain.OnboardingData{TermsVersion: cmd.TermsVersion, PrivacyVersion: cmd.PrivacyVersion}
	data.DisplayName = normalizeName(cmd.DisplayName, "display_name", &ve)
	if c, err := kernel.NewCountryCode(cmd.Country); err != nil {
		ve.Add("country", "INVALID")
	} else {
		data.Country = c
	}
	if l, err := kernel.NewLocale(cmd.Locale); err != nil {
		ve.Add("locale", "INVALID")
	} else {
		data.Locale = l
	}
	if cmd.Phone != nil && strings.TrimSpace(*cmd.Phone) != "" {
		ph, err := kernel.NewPhone(*cmd.Phone)
		if err != nil {
			ve.Add("phone", "INVALID")
		} else {
			data.Phone = &ph
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return MeView{}, err
	}
	acc, err := s.repo.Get(ctx, p.UserID)
	if err != nil {
		return MeView{}, err
	}
	cur, err := s.currentConsent(ctx)
	if err != nil {
		return MeView{}, err
	}
	now := s.now()
	if err := acc.CompleteOnboarding(data, cur, now); err != nil {
		return MeView{}, err
	}
	entry := s.entry(p, cmd.CorrelationID, cmd.IPHash, "me.onboarding_completed", map[string]any{
		"terms_version": data.TermsVersion, "privacy_version": data.PrivacyVersion,
	})
	if err := s.repo.Save(ctx, acc, entry); err != nil {
		return MeView{}, err
	}
	return MeView{Account: acc, Current: cur}, nil
}

// UpdateProfileCommand es PATCH /me: puntero nil = ausente; Clear* = null explícito.
type UpdateProfileCommand struct {
	DisplayName           *string
	Locale                *string
	Country               *string
	ClearCountry          bool
	Phone                 *string
	ClearPhone            bool
	Address               *kernel.Address
	ClearAddress          bool
	EmergencyContact      *kernel.Contact
	ClearEmergencyContact bool
	CorrelationID         *uuid.UUID
	IPHash                []byte
}

// UpdateProfile ejecuta me:update sobre el perfil.
func (s *Service) UpdateProfile(ctx context.Context, p auth.Principal, cmd UpdateProfileCommand) (MeView, error) {
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return MeView{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	var ve kernel.ValidationError
	changes := domain.ProfileChanges{
		ClearCountry: cmd.ClearCountry, ClearPhone: cmd.ClearPhone,
		ClearAddress: cmd.ClearAddress, ClearEmergencyContact: cmd.ClearEmergencyContact,
	}
	if cmd.DisplayName != nil {
		n := normalizeName(*cmd.DisplayName, "display_name", &ve)
		changes.DisplayName = &n
	}
	if cmd.Locale != nil {
		if l, err := kernel.NewLocale(*cmd.Locale); err != nil {
			ve.Add("locale", "INVALID")
		} else {
			changes.Locale = &l
		}
	}
	if cmd.Country != nil {
		if c, err := kernel.NewCountryCode(*cmd.Country); err != nil {
			ve.Add("country", "INVALID")
		} else {
			changes.Country = &c
		}
	}
	if cmd.Phone != nil {
		if ph, err := kernel.NewPhone(*cmd.Phone); err != nil {
			ve.Add("phone", "INVALID")
		} else {
			changes.Phone = &ph
		}
	}
	if cmd.Address != nil {
		if a, ok := normalizeAddress(*cmd.Address, &ve); ok {
			changes.Address = &a
		}
	}
	if cmd.EmergencyContact != nil {
		if c, ok := normalizeContact(*cmd.EmergencyContact, &ve); ok {
			changes.EmergencyContact = &c
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return MeView{}, err
	}
	acc, err := s.repo.Get(ctx, p.UserID)
	if err != nil {
		return MeView{}, err
	}
	cur, err := s.currentConsent(ctx)
	if err != nil {
		return MeView{}, err
	}
	fields, err := acc.UpdateProfile(changes, s.now())
	if err != nil {
		return MeView{}, err
	}
	if len(fields) == 0 {
		return MeView{Account: acc, Current: cur}, nil
	}
	entry := s.entry(p, cmd.CorrelationID, cmd.IPHash, "me.profile_updated", map[string]any{"fields": fields})
	if err := s.repo.Save(ctx, acc, entry); err != nil {
		return MeView{}, err
	}
	return MeView{Account: acc, Current: cur}, nil
}

// AcceptConsentCommand es POST /me/consent.
type AcceptConsentCommand struct {
	TermsVersion   string
	PrivacyVersion string
	CorrelationID  *uuid.UUID
	IPHash         []byte
}

// AcceptConsent ejecuta me:update sobre el consentimiento.
func (s *Service) AcceptConsent(ctx context.Context, p auth.Principal, cmd AcceptConsentCommand) error {
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return &kernel.ForbiddenError{Reason: d.Reason}
	}
	acc, err := s.repo.Get(ctx, p.UserID)
	if err != nil {
		return err
	}
	cur, err := s.currentConsent(ctx)
	if err != nil {
		return err
	}
	if err := acc.AcceptConsent(cmd.TermsVersion, cmd.PrivacyVersion, cur, s.now()); err != nil {
		return err
	}
	entry := s.entry(p, cmd.CorrelationID, cmd.IPHash, "me.consent_accepted", map[string]any{
		"terms_version": cmd.TermsVersion, "privacy_version": cmd.PrivacyVersion,
	})
	return s.repo.Save(ctx, acc, entry)
}

func (s *Service) entry(p auth.Principal, corr *uuid.UUID, ipHash []byte, action string, details map[string]any) *audit.Entry {
	actor := p.UserID
	target := p.UserID
	return &audit.Entry{
		ActorID: &actor, ActorRoles: p.GlobalRoles, Action: action,
		TargetType: "user_account", TargetID: &target, Details: details,
		CorrelationID: corr, IPHash: ipHash,
	}
}

func normalizeName(s, field string, ve *kernel.ValidationError) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" || len([]rune(s)) > domain.MaxDisplayName || hasControl(s) {
		ve.Add(field, "INVALID")
	}
	return s
}

func normalizeAddress(a kernel.Address, ve *kernel.ValidationError) (kernel.Address, bool) {
	ok := true
	c, err := kernel.NewCountryCode(string(a.Country))
	if err != nil {
		ve.Add("address.country", "INVALID")
		ok = false
	}
	out := kernel.Address{
		Country: c, Region: clean(a.Region), City: clean(a.City), Line1: clean(a.Line1),
		Line2: clean(a.Line2), PostalCode: clean(a.PostalCode),
	}
	if out.City == "" || hasControl(out.City) {
		ve.Add("address.city", "INVALID")
		ok = false
	}
	if out.Line1 == "" || hasControl(out.Line1) {
		ve.Add("address.line1", "INVALID")
		ok = false
	}
	return out, ok
}

func normalizeContact(c kernel.Contact, ve *kernel.ValidationError) (kernel.Contact, bool) {
	ok := true
	out := kernel.Contact{Name: normalizeName(c.Name, "emergency_contact.name", ve), Relationship: clean(c.Relationship)}
	if out.Name == "" {
		ok = false
	}
	ph, err := kernel.NewPhone(string(c.Phone))
	if err != nil {
		ve.Add("emergency_contact.phone", "INVALID")
		ok = false
	}
	out.Phone = ph
	return out, ok
}

func clean(s string) string { return strings.TrimSpace(s) }

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return true
		}
	}
	return false
}
