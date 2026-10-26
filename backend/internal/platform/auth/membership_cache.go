package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MembershipCache envuelve un MembershipLoader con caché en memoria por
// UserID (IAM-008): TTL 60 s, cota de entradas con expulsión LRU. En F3 pasa
// a Redis (ADR-014). La invalidación por eventos (DOM-011) la hace
// InvalidateUsers: un miembro retirado pierde acceso en el siguiente request
// (PRD-014).
type MembershipCache struct {
	loader MembershipLoader
	ttl    time.Duration
	max    int
	now    func() time.Time

	mu    sync.Mutex
	byUID map[uuid.UUID]*entry // índice por usuario
	head  *entry               // LRU: head = más reciente
	tail  *entry
}

type entry struct {
	userID      uuid.UUID
	memberships []Membership
	expiresAt   time.Time
	prev, next  *entry
}

// NewMembershipCache construye la caché. ttl 0 = 60 s; max 0 = 10 000.
func NewMembershipCache(loader MembershipLoader, ttl time.Duration, max int) *MembershipCache {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if max <= 0 {
		max = 10_000
	}
	return &MembershipCache{
		loader: loader, ttl: ttl, max: max, now: time.Now,
		byUID: map[uuid.UUID]*entry{},
	}
}

// Memberships implementa MembershipLoader con lectura-through.
func (c *MembershipCache) Memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.byUID[userID]; ok {
		if now.Before(e.expiresAt) {
			c.touch(e)
			ms := e.memberships
			c.mu.Unlock()
			return ms, nil
		}
		c.remove(e)
	}
	c.mu.Unlock()

	ms, err := c.loader.Memberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.store(userID, ms, now)
	c.mu.Unlock()
	return ms, nil
}

// InvalidateUsers borra las entradas de los usuarios indicados (DOM-011:
// reacciones a MemberJoined/RoleChanged/Removed y cambios de estado de la org).
func (c *MembershipCache) InvalidateUsers(ids []uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		if e, ok := c.byUID[id]; ok {
			c.remove(e)
		}
	}
}

func (c *MembershipCache) store(userID uuid.UUID, ms []Membership, now time.Time) {
	if e, ok := c.byUID[userID]; ok {
		e.memberships = ms
		e.expiresAt = now.Add(c.ttl)
		c.touch(e)
		return
	}
	e := &entry{userID: userID, memberships: ms, expiresAt: now.Add(c.ttl)}
	c.byUID[userID] = e
	c.pushFront(e)
	if len(c.byUID) > c.max {
		c.evictOldest()
	}
}

// lista doblemente enlazada LRU: head = más reciente.

func (c *MembershipCache) pushFront(e *entry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *MembershipCache) unlink(e *entry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (c *MembershipCache) touch(e *entry) {
	c.unlink(e)
	c.pushFront(e)
}

func (c *MembershipCache) remove(e *entry) {
	c.unlink(e)
	delete(c.byUID, e.userID)
}

func (c *MembershipCache) evictOldest() {
	if c.tail == nil {
		return
	}
	c.remove(c.tail)
}
