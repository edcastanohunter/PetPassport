package egress

import (
	"net"
	"testing"
	"time"
)

func TestEgress_Allowlist(t *testing.T) {
	client := NewClient(Config{
		AllowedHosts: []string{"api.anthropic.com", "*.anthropic.com"},
	})

	if err := client.CheckAllowed("https://api.anthropic.com/v1/messages"); err != nil {
		t.Errorf("expected api.anthropic.com to be allowed, got: %v", err)
	}

	if err := client.CheckAllowed("https://sub.anthropic.com/test"); err != nil {
		t.Errorf("expected wildcard host to be allowed, got: %v", err)
	}

	if err := client.CheckAllowed("https://evil.com/steal"); err == nil {
		t.Errorf("expected evil.com to be denied")
	}
}

func TestEgress_CircuitBreaker(t *testing.T) {
	client := NewClient(Config{
		AllowedHosts:      []string{"api.anthropic.com"},
		CircuitThreshold:  2,
		CircuitResetAfter: 100 * time.Millisecond,
	})

	host := "api.anthropic.com"
	if err := client.CheckAllowed("https://" + host); err != nil {
		t.Fatalf("expected host allowed, got %v", err)
	}

	// 1 failure
	client.RecordFailure(host)
	if err := client.CheckAllowed("https://" + host); err != nil {
		t.Fatalf("circuit should still be closed after 1 failure, got %v", err)
	}

	// 2 failures -> open
	client.RecordFailure(host)
	if err := client.CheckAllowed("https://" + host); err == nil {
		t.Fatalf("expected circuit to be open after 2 failures")
	}

	// Wait for reset
	time.Sleep(120 * time.Millisecond)
	if err := client.CheckAllowed("https://" + host); err != nil {
		t.Fatalf("expected circuit to allow trial request after reset window: %v", err)
	}

	// Success resets
	client.RecordSuccess(host)
	if err := client.CheckAllowed("https://" + host); err != nil {
		t.Fatalf("circuit should be closed after success")
	}
}

func TestEgress_RestrictedIPs(t *testing.T) {
	cases := []struct {
		ip         string
		restricted bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}

	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if got := isRestrictedIP(ip); got != c.restricted {
			t.Errorf("isRestrictedIP(%s) = %v, want %v", c.ip, got, c.restricted)
		}
	}
}
