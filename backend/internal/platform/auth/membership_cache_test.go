package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type countingLoader struct {
	mu    sync.Mutex
	calls map[uuid.UUID]int
	fail  error
}

func (l *countingLoader) Memberships(_ context.Context, id uuid.UUID) ([]Membership, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return nil, l.fail
	}
	l.calls[id]++
	return []Membership{{OrganizationID: id, Role: "veterinarian", OrgStatus: "active"}}, nil
}

func (l *countingLoader) count(id uuid.UUID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[id]
}

func newTestCache(ttl time.Duration, max int) (*MembershipCache, *countingLoader, *time.Time) {
	l := &countingLoader{calls: map[uuid.UUID]int{}}
	c := NewMembershipCache(l, ttl, max)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, l, &now
}

func TestMembershipCache_IAM008_TTL60s(t *testing.T) {
	c, l, now := newTestCache(0, 0) // 0 → 60 s por defecto
	ctx := context.Background()
	u := uuid.New()
	for i := 0; i < 3; i++ {
		if _, err := c.Memberships(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if l.count(u) != 1 {
		t.Fatalf("lecturas al read model dentro del TTL: %d", l.count(u))
	}
	*now = now.Add(59 * time.Second)
	_, _ = c.Memberships(ctx, u)
	if l.count(u) != 1 {
		t.Fatal("a los 59 s debe seguir en caché")
	}
	*now = now.Add(2 * time.Second)
	_, _ = c.Memberships(ctx, u)
	if l.count(u) != 2 {
		t.Fatalf("tras 61 s debe recargar: %d", l.count(u))
	}
}

func TestMembershipCache_DOM011_InvalidationReloadsNextRequest(t *testing.T) {
	c, l, _ := newTestCache(time.Minute, 10)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	_, _ = c.Memberships(ctx, a)
	_, _ = c.Memberships(ctx, b)
	c.InvalidateUsers([]uuid.UUID{a, uuid.New()}) // ids desconocidos se ignoran
	_, _ = c.Memberships(ctx, a)
	_, _ = c.Memberships(ctx, b)
	if l.count(a) != 2 || l.count(b) != 1 {
		t.Fatalf("invalidación: a=%d b=%d", l.count(a), l.count(b))
	}
}

func TestMembershipCache_LRUEviction(t *testing.T) {
	c, l, _ := newTestCache(time.Minute, 2)
	ctx := context.Background()
	a, b, d := uuid.New(), uuid.New(), uuid.New()
	_, _ = c.Memberships(ctx, a)
	_, _ = c.Memberships(ctx, b)
	_, _ = c.Memberships(ctx, a) // a pasa a ser el más reciente
	_, _ = c.Memberships(ctx, d) // expulsa b (el menos reciente)
	_, _ = c.Memberships(ctx, a)
	_, _ = c.Memberships(ctx, b)
	if l.count(a) != 1 || l.count(b) != 2 || l.count(d) != 1 {
		t.Fatalf("LRU: a=%d b=%d d=%d", l.count(a), l.count(b), l.count(d))
	}
	if len(c.byUID) > 2 {
		t.Fatalf("cota superada: %d entradas", len(c.byUID))
	}
}

func TestMembershipCache_ErrorsAreNotCached(t *testing.T) {
	c, l, _ := newTestCache(time.Minute, 10)
	l.fail = errors.New("db caída")
	u := uuid.New()
	if _, err := c.Memberships(context.Background(), u); err == nil {
		t.Fatal("debe propagar el error")
	}
	l.fail = nil
	if _, err := c.Memberships(context.Background(), u); err != nil || l.count(u) != 1 {
		t.Fatalf("tras el error debe reintentar: err=%v n=%d", err, l.count(u))
	}
}

func TestMembershipCache_ConcurrentAccess(t *testing.T) {
	c, _, _ := newTestCache(time.Minute, 50)
	ids := make([]uuid.UUID, 100)
	for i := range ids {
		ids[i] = uuid.New()
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				id := ids[(i*7+g)%len(ids)]
				if i%13 == 0 {
					c.InvalidateUsers([]uuid.UUID{id})
					continue
				}
				_, _ = c.Memberships(context.Background(), id)
			}
		}(g)
	}
	wg.Wait()
	if len(c.byUID) > 50 {
		t.Fatalf("cota superada: %d", len(c.byUID))
	}
}
