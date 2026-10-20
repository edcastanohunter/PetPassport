package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

const aud = "petpassport-api"

func newVerifier(t *testing.T, j *testkit.JWKS, azp ...string) *auth.Verifier {
	t.Helper()
	v, err := auth.NewVerifier(context.Background(), auth.VerifierConfig{Issuer: j.Issuer, Audience: aud, AllowedAZP: azp})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerifier_AcceptsValidToken(t *testing.T) {
	j := testkit.NewJWKS(t)
	v := newVerifier(t, j, "petpassport-web")
	tok := j.Token(t, testkit.Claims{
		Subject: "0192f3a2-2222-7aaa-bbbb-dddddddddddd", Email: "ana@example.com", EmailVerified: true,
		Audience: aud, Roles: []string{"pet_owner"},
	})
	c, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Subject != "0192f3a2-2222-7aaa-bbbb-dddddddddddd" || !c.EmailVerified || c.Roles()[0] != "pet_owner" {
		t.Fatalf("claims inesperados: %+v", c)
	}
}

func TestVerifier_RejectsInvalidTokens(t *testing.T) {
	j := testkit.NewJWKS(t)
	other := testkit.NewJWKS(t)
	v := newVerifier(t, j, "petpassport-web")
	cases := map[string]string{
		"aud incorrecta":    j.Token(t, testkit.Claims{Subject: "u", Audience: "otro"}),
		"expirado":          j.Token(t, testkit.Claims{Subject: "u", Audience: aud, TTL: -time.Minute * 5}),
		"azp no permitido":  j.Token(t, testkit.Claims{Subject: "u", Audience: aud, AuthorizedParty: "evil"}),
		"sin sub":           j.Token(t, testkit.Claims{Audience: aud}),
		"typ Refresh":       j.Token(t, testkit.Claims{Subject: "u", Audience: aud, Extra: map[string]any{"typ": "Refresh"}}),
		"nbf futuro":        j.Token(t, testkit.Claims{Subject: "u", Audience: aud, Extra: map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}}),
		"otro emisor/clave": other.Token(t, testkit.Claims{Subject: "u", Audience: aud}),
		"alg none":          noneToken(j.Issuer),
		"basura":            "abc.def",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tok)
			if !errors.Is(err, auth.ErrInvalidToken) {
				t.Fatalf("esperaba ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestVerifier_DiscoveryDistinctFromIssuer(t *testing.T) {
	j := testkit.NewJWKS(t)
	// El documento interno anuncia j.Issuer; configuramos ese issuer pero con una
	// URL de descubrimiento explícita (misma en pruebas), como en compose.
	v, err := auth.NewVerifier(context.Background(), auth.VerifierConfig{
		Issuer: j.Issuer, DiscoveryURL: j.Issuer + "/", Audience: aud,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), j.Token(t, testkit.Claims{Subject: "u", Audience: aud})); err != nil {
		t.Fatal(err)
	}
}

func TestVerifier_RequiresIssuerAndAudience(t *testing.T) {
	if _, err := auth.NewVerifier(context.Background(), auth.VerifierConfig{}); err == nil {
		t.Fatal("esperaba error")
	}
}

func TestBearerToken(t *testing.T) {
	if tok, ok := auth.BearerToken("Bearer abc"); !ok || tok != "abc" {
		t.Fatal("Bearer abc")
	}
	if tok, ok := auth.BearerToken("bearer   abc "); !ok || tok != "abc" {
		t.Fatal("bearer minúsculas")
	}
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic abc", "abc"} {
		if _, ok := auth.BearerToken(h); ok {
			t.Fatalf("aceptó %q", h)
		}
	}
}

func noneToken(iss string) string {
	h, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	p, _ := json.Marshal(map[string]any{"iss": iss, "sub": "u", "aud": aud, "exp": time.Now().Add(time.Hour).Unix()})
	enc := func(b []byte) string { return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=") }
	return enc(h) + "." + enc(p) + "."
}
