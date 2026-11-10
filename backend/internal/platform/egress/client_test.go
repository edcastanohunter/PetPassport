package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

type mockResolver struct {
	ips map[string][]net.IP
	err error
}

func (m *mockResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if m.err != nil {
		return nil, m.err
	}
	if ips, ok := m.ips[host]; ok {
		return ips, nil
	}
	return nil, errors.New("host not found in mock")
}

func TestEgress_ValidateWebhookURL(t *testing.T) {
	res := &mockResolver{
		ips: map[string][]net.IP{
			"valid.partner.com":    {net.ParseIP("93.184.216.34")},
			"internal.partner.com": {net.ParseIP("10.0.1.20")},
			"metadata.evil.com":    {net.ParseIP("169.254.169.254")},
			"localhost.evil.com":   {net.ParseIP("127.0.0.1")},
		},
	}

	tests := []struct {
		name    string
		rawURL  string
		wantErr error
	}{
		{
			name:    "valid https default port",
			rawURL:  "https://valid.partner.com/webhooks/petpassport",
			wantErr: nil,
		},
		{
			name:    "valid https 8443 port",
			rawURL:  "https://valid.partner.com:8443/webhooks",
			wantErr: nil,
		},
		{
			name:    "invalid scheme http",
			rawURL:  "http://valid.partner.com/webhooks",
			wantErr: ErrInvalidScheme,
		},
		//nolint:gosec // intentional test of userinfo rejection
		{
			name:    "userinfo credentials rejected",
			rawURL:  "https://user:pass@valid.partner.com/webhooks",
			wantErr: ErrUserinfoNotAllowed,
		},
		{
			name:    "literal IP rejected",
			rawURL:  "https://127.0.0.1/webhooks",
			wantErr: ErrLiteralIPBlocked,
		},
		{
			name:    "invalid port rejected",
			rawURL:  "https://valid.partner.com:8080/webhooks",
			wantErr: ErrInvalidPort,
		},
		{
			name:    "internal private ip ssrf blocked",
			rawURL:  "https://internal.partner.com/webhooks",
			wantErr: ErrSSRFBlocked,
		},
		{
			name:    "cloud metadata ssrf blocked",
			rawURL:  "https://metadata.evil.com/webhooks",
			wantErr: ErrSSRFBlocked,
		},
		{
			name:    "localhost ssrf blocked",
			rawURL:  "https://localhost.evil.com/webhooks",
			wantErr: ErrSSRFBlocked,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWebhookURLWithResolver(context.Background(), tc.rawURL, res)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %v, got nil", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) && !strings.Contains(err.Error(), tc.wantErr.Error()) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestEgress_WebhookNoRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	defer ts.Close()

	client := NewWebhookClient(Config{
		AllowedHosts:  []string{"*"},
		AllowLoopback: true,
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	// El cliente de webhooks tiene MaxRedirects=0 y devuelve ErrUseLastResponse (no sigue el 302)
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected status 302 (no-redirect), got %d", resp.StatusCode)
	}
}
