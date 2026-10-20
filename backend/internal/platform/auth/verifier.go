package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// VerifierConfig configura la validación de tokens (docs/phase-1/04 §7).
type VerifierConfig struct {
	Issuer       string   // iss esperado (p. ej. https://auth.<dominio>/realms/petpassport)
	DiscoveryURL string   // URL de descubrimiento si difiere del issuer (red interna); vacío = Issuer
	Audience     string   // aud requerido (petpassport-api)
	AllowedAZP   []string // azp permitidos; vacío = no se comprueba
	Skew         time.Duration
}

// Verifier valida access tokens y devuelve sus claims.
type Verifier struct {
	cfg      VerifierConfig
	verifier *oidc.IDTokenVerifier
}

// ErrInvalidToken agrupa cualquier fallo de validación; el detalle va a logs,
// nunca al cliente.
var ErrInvalidToken = errors.New("invalid token")

// NewVerifier descarga el documento de descubrimiento (y las claves bajo
// demanda). Falla si el proveedor no responde: el proceso no debe arrancar sin él.
func NewVerifier(ctx context.Context, cfg VerifierConfig) (*Verifier, error) {
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("auth: issuer y audience son obligatorios")
	}
	if cfg.Skew == 0 {
		cfg.Skew = 60 * time.Second
	}
	discovery := cfg.DiscoveryURL
	if discovery == "" {
		discovery = cfg.Issuer
	}
	pctx := ctx
	if discovery != cfg.Issuer {
		// El documento de descubrimiento interno puede anunciar otro issuer; el
		// token sigue validándose contra cfg.Issuer.
		pctx = oidc.InsecureIssuerURLContext(ctx, cfg.Issuer)
	}
	provider, err := oidc.NewProvider(pctx, discovery)
	if err != nil {
		return nil, fmt.Errorf("auth: discovery %s: %w", discovery, err)
	}
	v := provider.VerifierContext(ctx, &oidc.Config{
		ClientID:             cfg.Audience,
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256},
	})
	return &Verifier{cfg: cfg, verifier: v}, nil
}

// Verify valida firma, iss, aud, exp y las reglas adicionales de IAM-007.
func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	tok, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return Claims{}, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	if c.Subject == "" {
		return Claims{}, fmt.Errorf("%w: sin sub", ErrInvalidToken)
	}
	if c.Type != "" && !strings.EqualFold(c.Type, "Bearer") {
		return Claims{}, fmt.Errorf("%w: typ %q", ErrInvalidToken, c.Type)
	}
	if len(v.cfg.AllowedAZP) > 0 && !slices.Contains(v.cfg.AllowedAZP, c.AuthorizedParty) {
		return Claims{}, fmt.Errorf("%w: azp %q no permitido", ErrInvalidToken, c.AuthorizedParty)
	}
	now := time.Now()
	if c.NotBefore > 0 && time.Unix(c.NotBefore, 0).After(now.Add(v.cfg.Skew)) {
		return Claims{}, fmt.Errorf("%w: nbf en el futuro", ErrInvalidToken)
	}
	if c.IssuedAt > 0 && time.Unix(c.IssuedAt, 0).After(now.Add(v.cfg.Skew)) {
		return Claims{}, fmt.Errorf("%w: iat en el futuro", ErrInvalidToken)
	}
	return c, nil
}

// BearerToken extrae el token de la cabecera Authorization (solo esquema Bearer).
func BearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	return tok, tok != ""
}
