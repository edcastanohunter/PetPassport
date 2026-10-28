// Package keycloakadmin implementa el ACL identity.KeycloakAdmin sobre la
// Admin REST API de Keycloak con la service account `petpassport-backend`
// (PP-F1-IAM-011, docs/phase-1/04 §9). Mínimo privilegio: la service account
// solo tiene view-users y manage-users de realm-management; este cliente no
// crea usuarios ni roles.
package keycloakadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
)

// tokenSkew: el token se renueva 60 s antes de expirar (IAM-011).
const tokenSkew = 60 * time.Second

// Options configura el cliente.
type Options struct {
	// BaseURL de Keycloak (p. ej. http://keycloak:8080), sin /realms.
	BaseURL      string
	Realm        string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	Now          func() time.Time
}

// Client es el cliente de la Admin API.
type Client struct {
	opts Options

	mu     sync.Mutex
	token  string
	expiry time.Time
}

var _ app.KeycloakAdmin = (*Client)(nil)

// New construye el cliente; falla si falta configuración.
func New(opts Options) (*Client, error) {
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.BaseURL == "" || opts.Realm == "" || opts.ClientID == "" || opts.ClientSecret == "" {
		return nil, errors.New("keycloakadmin: BaseURL, Realm, ClientID y ClientSecret son obligatorios")
	}
	if _, err := url.ParseRequestURI(opts.BaseURL); err != nil {
		return nil, fmt.Errorf("keycloakadmin: BaseURL inválida: %w", err)
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Client{opts: opts}, nil
}

// SetEnabled habilita o deshabilita el usuario (PUT /users/{id} {enabled}).
func (c *Client) SetEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error {
	body, _ := json.Marshal(map[string]bool{"enabled": enabled})
	return c.do(ctx, http.MethodPut, c.userPath(userID), body, nil)
}

// LogoutAllSessions revoca todas las sesiones del usuario (POST /users/{id}/logout).
func (c *Client) LogoutAllSessions(ctx context.Context, userID uuid.UUID) error {
	return c.do(ctx, http.MethodPost, c.userPath(userID)+"/logout", nil, nil)
}

// SendVerifyEmail reenvía el email de verificación (PUT /users/{id}/send-verify-email).
func (c *Client) SendVerifyEmail(ctx context.Context, userID uuid.UUID) error {
	return c.do(ctx, http.MethodPut, c.userPath(userID)+"/send-verify-email", nil, nil)
}

// FindByEmail devuelve el id del usuario con ese email exacto (solo admin).
func (c *Client) FindByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	q := url.Values{"email": {email}, "exact": {"true"}}
	var users []struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, c.adminBase()+"/users?"+q.Encode(), nil, &users); err != nil {
		return uuid.Nil, err
	}
	if len(users) == 0 {
		return uuid.Nil, app.ErrKeycloakUserNotFound
	}
	return uuid.Parse(users[0].ID)
}

func (c *Client) adminBase() string {
	return c.opts.BaseURL + "/admin/realms/" + url.PathEscape(c.opts.Realm)
}

func (c *Client) userPath(id uuid.UUID) string { return c.adminBase() + "/users/" + id.String() }

// do ejecuta la llamada con el token de la service account; ante 401
// invalida el token y reintenta una vez (token revocado o rotado).
func (c *Client) do(ctx context.Context, method, target string, body []byte, out any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return err
		}
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		status, err := c.send(req, out)
		if status == 0 {
			return fmt.Errorf("keycloakadmin %s: %w", method, err)
		}
		switch {
		case status == http.StatusUnauthorized && attempt == 0:
			c.invalidate()
			continue
		case status == http.StatusNotFound:
			return app.ErrKeycloakUserNotFound
		}
		return err
	}
}

// send ejecuta la request y decodifica la respuesta; status 0 = error de red.
func (c *Client) send(req *http.Request, out any) (int, error) {
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// El cuerpo de error de Keycloak no se registra (puede incluir datos del usuario).
		return resp.StatusCode, fmt.Errorf("keycloakadmin: status %d", resp.StatusCode)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("keycloakadmin: respuesta inválida: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// accessToken devuelve el token cacheado o pide uno nuevo con client
// credentials (válido hasta tokenSkew antes de expirar).
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.opts.Now()
	if c.token != "" && now.Before(c.expiry) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.opts.ClientID},
		"client_secret": {c.opts.ClientSecret},
	}
	endpoint := c.opts.BaseURL + "/realms/" + url.PathEscape(c.opts.Realm) + "/protocol/openid-connect/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if _, err := c.send(req, &tok); err != nil {
		return "", fmt.Errorf("keycloakadmin token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("keycloakadmin token: respuesta sin access_token")
	}
	ttl := time.Duration(tok.ExpiresIn)*time.Second - tokenSkew
	if ttl < 0 {
		ttl = 0
	}
	c.token, c.expiry = tok.AccessToken, now.Add(ttl)
	return c.token, nil
}

func (c *Client) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expiry = "", time.Time{}
}
