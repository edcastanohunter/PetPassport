package testkit

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestJWKS_ServesDiscoveryAndSignsTokens(t *testing.T) {
	j := NewJWKS(t)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, j.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var disc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		t.Fatal(err)
	}
	if disc["issuer"] != j.Issuer {
		t.Fatalf("issuer mismatch: %v", disc["issuer"])
	}
	tok := j.Token(t, Claims{Subject: "u1", Email: "owner@dev.local", EmailVerified: true, Audience: "petpassport-api", Roles: []string{"pet_owner"}})
	if parts := strings.Split(tok, "."); len(parts) != 3 {
		t.Fatalf("token should have 3 parts, got %d", len(parts))
	}
}
