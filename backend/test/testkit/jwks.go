package testkit

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// JWKS es un emisor OIDC de prueba: sirve /.well-known/openid-configuration y
// /jwks y firma tokens RS256 con una clave generada por proceso. Permite probar
// la validación de tokens (M1.1) sin Keycloak.
type JWKS struct {
	Server *httptest.Server
	Issuer string
	key    *rsa.PrivateKey
	kid    string
}

// NewJWKS arranca el emisor de prueba.
func NewJWKS(t testing.TB) *JWKS {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	j := &JWKS{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                j.Issuer,
			"jwks_uri":                              j.Issuer + "/jwks",
			"authorization_endpoint":                j.Issuer + "/auth",
			"token_endpoint":                        j.Issuer + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": j.kid,
			"n": b64(key.N.Bytes()),
			"e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	j.Server = httptest.NewServer(mux)
	j.Issuer = j.Server.URL
	t.Cleanup(j.Server.Close)
	return j
}

// Claims describe los claims del token de prueba.
type Claims struct {
	Subject         string
	Email           string
	EmailVerified   bool
	Name            string
	Audience        string
	Roles           []string
	AuthorizedParty string
	TTL             time.Duration
	Extra           map[string]any
}

// Token firma un JWT RS256 con los claims dados (estructura equivalente a la
// que emite Keycloak: realm_access.roles, aud, azp, typ Bearer).
func (j *JWKS) Token(t testing.TB, c Claims) string {
	t.Helper()
	now := time.Now()
	ttl := c.TTL
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	azp := c.AuthorizedParty
	if azp == "" {
		azp = "petpassport-web"
	}
	payload := map[string]any{
		"iss": j.Issuer, "sub": c.Subject, "aud": c.Audience, "azp": azp, "typ": "Bearer",
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(ttl).Unix(),
		"email": c.Email, "email_verified": c.EmailVerified, "name": c.Name,
		"preferred_username": c.Email, "sid": "test-session",
		"realm_access": map[string]any{"roles": c.Roles},
	}
	for k, v := range c.Extra {
		payload[k] = v
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": j.kid}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(payload)
	signingInput := b64(hb) + "." + b64(pb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, j.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + b64(sig)
}

func b64(b []byte) string { return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=") }
