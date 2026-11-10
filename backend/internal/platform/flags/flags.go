package flags

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
)

// Flag represents a feature flag stored in platform.feature_flags (PP-F5-OPS-010).
type Flag struct {
	Key               string         `json:"key"`
	Enabled           bool           `json:"enabled"`
	RolloutPercentage int            `json:"rollout_percentage"`
	Targeting         map[string]any `json:"targeting"`
	Description       string         `json:"description"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// EvaluationContext contains caller identity and metadata for flag targeting.
type EvaluationContext struct {
	UserID     string            `json:"user_id,omitempty"`
	OrgID      string            `json:"org_id,omitempty"`
	Role       string            `json:"role,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Store defines access to feature flag configurations.
type Store interface {
	GetAll(ctx context.Context) ([]Flag, error)
	Get(ctx context.Context, key string) (*Flag, error)
}

// Evaluator evaluates feature flags.
type Evaluator interface {
	IsEnabled(ctx context.Context, flagKey string, evalCtx EvaluationContext) bool
	AllFlags(ctx context.Context, evalCtx EvaluationContext) map[string]bool
}

// MemoryStore is an in-memory thread-safe implementation of Store.
type MemoryStore struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore(initial []Flag) *MemoryStore {
	m := &MemoryStore{flags: make(map[string]Flag)}
	for _, f := range initial {
		m.flags[f.Key] = f
	}
	return m
}

// Set updates or inserts a flag.
func (m *MemoryStore) Set(f Flag) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flags[f.Key] = f
}

// GetAll returns all flags.
func (m *MemoryStore) GetAll(_ context.Context) ([]Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]Flag, 0, length(m.flags))
	for _, f := range m.flags {
		res = append(res, f)
	}
	return res, nil
}

// Get returns a single flag by key.
func (m *MemoryStore) Get(_ context.Context, key string) (*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	if !ok {
		return nil, fmt.Errorf("flag not found: %s", key)
	}
	return &f, nil
}

func length(m map[string]Flag) int {
	return len(m)
}

// Engine implements Evaluator using a Store and caching.
type Engine struct {
	store Store
}

// NewEngine creates a new flag evaluation Engine.
func NewEngine(store Store) *Engine {
	return &Engine{store: store}
}

// IsEnabled evaluates whether a specific flag is active for the given context.
func (e *Engine) IsEnabled(ctx context.Context, flagKey string, evalCtx EvaluationContext) bool {
	f, err := e.store.Get(ctx, flagKey)
	if err != nil || f == nil {
		return false
	}
	return evaluateFlag(f, evalCtx)
}

// AllFlags evaluates all available flags for the given context.
func (e *Engine) AllFlags(ctx context.Context, evalCtx EvaluationContext) map[string]bool {
	result := make(map[string]bool)
	all, err := e.store.GetAll(ctx)
	if err != nil {
		return result
	}
	for _, f := range all {
		result[f.Key] = evaluateFlag(&f, evalCtx)
	}
	return result
}

func evaluateFlag(f *Flag, evalCtx EvaluationContext) bool {
	if !f.Enabled {
		return false
	}

	// 1. Check targeting rules if present
	if len(f.Targeting) > 0 {
		if allowedRoles, ok := f.Targeting["allowed_roles"].([]string); ok && len(allowedRoles) > 0 {
			match := false
			for _, r := range allowedRoles {
				if strings.EqualFold(r, evalCtx.Role) {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		}

		if allowedOrgs, ok := f.Targeting["allowed_orgs"].([]string); ok && len(allowedOrgs) > 0 {
			match := false
			for _, org := range allowedOrgs {
				if org == evalCtx.OrgID {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		}

		if allowedUsers, ok := f.Targeting["allowed_users"].([]string); ok && len(allowedUsers) > 0 {
			match := false
			for _, u := range allowedUsers {
				if u == evalCtx.UserID {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		}
	}

	// 2. Check rollout percentage (0-100)
	if f.RolloutPercentage >= 100 {
		return true
	}
	if f.RolloutPercentage <= 0 {
		return false
	}

	// Deterministic percentage hash based on user ID, org ID, or anonymous salt
	id := evalCtx.UserID
	if id == "" {
		id = evalCtx.OrgID
	}
	if id == "" {
		id = "anon"
	}

	bucket := deterministicBucket(f.Key, id)
	return bucket < f.RolloutPercentage
}

func deterministicBucket(flagKey, identifier string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(flagKey))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(identifier))
	return int(h.Sum32() % 100)
}
