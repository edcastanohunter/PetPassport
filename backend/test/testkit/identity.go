package testkit

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// MemoryIdentityRepo es un app.Repository en memoria para pruebas de aplicación
// y de la capa HTTP sin PostgreSQL.
type MemoryIdentityRepo struct {
	mu       sync.Mutex
	Accounts map[uuid.UUID]*domain.UserAccount
	Audit    []audit.Entry
	Consent  domain.ConsentVersions
	Saves    int
	FailWith error
}

// NewMemoryIdentityRepo construye el repositorio con las versiones de la semilla.
func NewMemoryIdentityRepo() *MemoryIdentityRepo {
	return &MemoryIdentityRepo{
		Accounts: map[uuid.UUID]*domain.UserAccount{},
		Consent: domain.ConsentVersions{
			Terms: "2026-10-draft", Privacy: "2026-10-draft", TermsURL: "/legal/terms", PrivacyURL: "/legal/privacy",
		},
	}
}

var _ app.Repository = (*MemoryIdentityRepo)(nil)

func clone(a *domain.UserAccount) *domain.UserAccount {
	c := *a
	c.GlobalRoles = append([]string(nil), a.GlobalRoles...)
	return &c
}

// Get implementa app.Repository.
func (m *MemoryIdentityRepo) Get(_ context.Context, id uuid.UUID) (*domain.UserAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return nil, m.FailWith
	}
	a, ok := m.Accounts[id]
	if !ok {
		return nil, &kernel.NotFoundError{Resource: "user_account"}
	}
	return clone(a), nil
}

// FindByEmail implementa app.Repository.
func (m *MemoryIdentityRepo) FindByEmail(_ context.Context, email kernel.Email) (*domain.UserAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return nil, m.FailWith
	}
	for _, a := range m.Accounts {
		if a.Email == email {
			return clone(a), nil
		}
	}
	return nil, &kernel.NotFoundError{Resource: "user_account"}
}

// Insert implementa app.Repository.
func (m *MemoryIdentityRepo) Insert(_ context.Context, a *domain.UserAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return m.FailWith
	}
	if _, ok := m.Accounts[a.ID]; ok {
		return app.ErrAlreadyExists
	}
	m.Accounts[a.ID] = clone(a)
	return nil
}

// Save implementa app.Repository.
func (m *MemoryIdentityRepo) Save(_ context.Context, a *domain.UserAccount, entry *audit.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return m.FailWith
	}
	if _, ok := m.Accounts[a.ID]; !ok {
		return &kernel.NotFoundError{Resource: "user_account"}
	}
	m.Accounts[a.ID] = clone(a)
	m.Saves++
	if entry != nil {
		m.Audit = append(m.Audit, *entry)
	}
	return nil
}

// CurrentConsent implementa app.Repository.
func (m *MemoryIdentityRepo) CurrentConsent(context.Context) (domain.ConsentVersions, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return domain.ConsentVersions{}, m.FailWith
	}
	return m.Consent, nil
}
