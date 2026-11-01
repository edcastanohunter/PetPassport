package redis

import (
	"errors"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestBreaker_Transitions(t *testing.T) {
	b := NewBreaker("test_db", 3, 50*time.Millisecond)

	if state := b.State(); state != StateClosed {
		t.Fatalf("expected closed, got %v", state)
	}

	// 2 failures: still closed
	b.RecordFailure()
	b.RecordFailure()
	if !b.Allow() {
		t.Fatal("expected allow while under threshold")
	}
	if b.State() != StateClosed {
		t.Fatalf("expected closed, got %v", b.State())
	}

	// 3rd failure: opens
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %v", b.State())
	}
	if b.Allow() {
		t.Fatal("expected deny when open")
	}

	// Wait for openDuration to expire
	time.Sleep(60 * time.Millisecond)

	// Next Allow should transition to HalfOpen
	if !b.Allow() {
		t.Fatal("expected allow after duration in half-open")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("expected half-open, got %v", b.State())
	}

	// Success in HalfOpen closes breaker
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Fatalf("expected closed after success, got %v", b.State())
	}
}

func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	b := NewBreaker("test_db", 2, 20*time.Millisecond)
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %v", b.State())
	}

	time.Sleep(30 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("expected allow in half-open")
	}

	// Failure in half-open immediately re-opens
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open after half-open failure, got %v", b.State())
	}
}

func TestClient_Do_CircuitBreaker(t *testing.T) {
	client, err := NewClient(Config{
		Addr:             "127.0.0.1:65530", // unreachable
		FailureThreshold: 2,
		OpenDuration:     100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	defer client.Close()

	customErr := errors.New("connection failed")

	// 1st fail
	_ = client.Do("get", func(r *goredis.Client) error {
		return customErr
	})
	// 2nd fail -> breaker opens
	_ = client.Do("get", func(r *goredis.Client) error {
		return customErr
	})

	if client.Breaker().State() != StateOpen {
		t.Fatalf("expected breaker to be open, got %v", client.Breaker().State())
	}

	// Next call returns ErrCircuitOpen without calling function
	called := false
	err = client.Do("get", func(r *goredis.Client) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Fatal("fn should not have been called when breaker is open")
	}
}
