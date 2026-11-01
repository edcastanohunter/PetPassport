package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyFamily(t *testing.T) {
	cases := []struct {
		key      string
		expected string
	}{
		{"pp:v1:membership:123", "membership"},
		{"pp:v1:petaccess:456", "petaccess"},
		{"pp:v1:pet:summary:789", "pet"},
		{"invalid", "other"},
	}

	for _, c := range cases {
		if got := KeyFamily(c.key); got != c.expected {
			t.Errorf("KeyFamily(%q) = %q, expected %q", c.key, got, c.expected)
		}
	}
}

func TestL1Cache_Basics(t *testing.T) {
	l1 := NewL1Cache(50 * time.Millisecond)

	l1.Set("k1", []byte("v1"))
	val, ok := l1.Get("k1")
	if !ok || string(val) != "v1" {
		t.Fatalf("expected v1, got %s (ok=%v)", val, ok)
	}

	l1.Delete("k1")
	_, ok = l1.Get("k1")
	if ok {
		t.Fatal("expected k1 to be deleted")
	}

	l1.Set("k2", []byte("v2"))
	time.Sleep(60 * time.Millisecond)
	_, ok = l1.Get("k2")
	if ok {
		t.Fatal("expected k2 to have expired")
	}
}

func TestCache_Fetch_Singleflight(t *testing.T) {
	c := NewCache(nil) // sin Redis, opera L1 + singleflight

	var loadCount int64
	loader := func(ctx context.Context) (any, error) {
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&loadCount, 1)
		return map[string]string{"foo": "bar"}, nil
	}

	var wg sync.WaitGroup
	ctx := context.Background()

	// 10 concurrent requests for the same key
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result map[string]string
			err := c.Fetch(ctx, "pp:v1:test:sf", 5*time.Second, &result, loader)
			if err != nil {
				t.Errorf("Fetch error: %v", err)
			}
			if result["foo"] != "bar" {
				t.Errorf("unexpected result: %v", result)
			}
		}()
	}

	wg.Wait()

	if count := atomic.LoadInt64(&loadCount); count != 1 {
		t.Fatalf("expected loader to be called exactly 1 time via singleflight, got %d", count)
	}
}

func TestCache_FailOpen_NilRedis(t *testing.T) {
	c := NewCache(nil)
	ctx := context.Background()

	var dest string
	err := c.Get(ctx, "pp:v1:membership:none", &dest)
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}

	// Set with nil Redis does not return error
	err = c.Set(ctx, "pp:v1:membership:user1", "val1", time.Minute)
	if err != nil {
		t.Fatalf("expected nil error on set, got %v", err)
	}

	// Should be available in L1
	err = c.Get(ctx, "pp:v1:membership:user1", &dest)
	if err != nil || dest != "val1" {
		t.Fatalf("expected val1 from L1, got %v (err=%v)", dest, err)
	}
}
