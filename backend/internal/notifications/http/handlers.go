// Package http expone la API de Notifications (PP-F2-API-010): preferencias del
// usuario, baja por enlace y el webhook firmado del proveedor de email.
package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PreferencesService es lo que la API necesita del módulo.
type PreferencesService interface {
	Get(ctx context.Context, user uuid.UUID) (domain.Preferences, error)
	Put(ctx context.Context, user uuid.UUID, p domain.Preferences) (domain.Preferences, error)
	Unsubscribe(ctx context.Context, token string) (domain.Category, error)
}

// Suppressor registra direcciones suprimidas (rebotes y quejas).
type Suppressor interface {
	Suppress(ctx context.Context, addressHash []byte, reason string) error
}

// NotificationHandlers implementa las operaciones de Notifications del contrato.
type NotificationHandlers struct {
	svc            PreferencesService
	supp           Suppressor
	webhookSecrets map[string][]byte // proveedor → secreto de firma
	logger         *slog.Logger
}

// NewNotificationHandlers construye los handlers. webhookSecrets vacío = los
// webhooks responden 404 (sin proveedor configurado, NOT-007).
func NewNotificationHandlers(svc PreferencesService, supp Suppressor, webhookSecrets map[string][]byte, logger *slog.Logger) *NotificationHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationHandlers{svc: svc, supp: supp, webhookSecrets: webhookSecrets, logger: logger}
}

func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "no autenticado"}
	}
	return p, nil
}

// GetNotificationPreferences devuelve las preferencias propias (me:read).
func (h *NotificationHandlers) GetNotificationPreferences(ctx context.Context, _ gen.GetNotificationPreferencesRequestObject) (gen.GetNotificationPreferencesResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	prefs, err := h.svc.Get(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return gen.GetNotificationPreferences200JSONResponse(toDTO(prefs)), nil
}

// PutNotificationPreferences guarda las preferencias propias (me:update).
func (h *NotificationHandlers) PutNotificationPreferences(ctx context.Context, req gen.PutNotificationPreferencesRequestObject) (gen.PutNotificationPreferencesResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	prefs, err := h.svc.Put(ctx, p.UserID, fromDTO(gen.NotificationPreferences(*req.Body)))
	if err != nil {
		return nil, err
	}
	return gen.PutNotificationPreferences200JSONResponse(toDTO(prefs)), nil
}

// UnsubscribeNotifications da de baja una categoría con el token de un correo.
// Sin sesión: el token es la credencial. Inválido o caducado → 404.
func (h *NotificationHandlers) UnsubscribeNotifications(ctx context.Context, req gen.UnsubscribeNotificationsRequestObject) (gen.UnsubscribeNotificationsResponseObject, error) {
	cat, err := h.svc.Unsubscribe(ctx, req.Token)
	if errors.Is(err, domain.ErrTokenInvalid) {
		return nil, &kernel.NotFoundError{Resource: "unsubscribe_token"}
	}
	if err != nil {
		return nil, err
	}
	return gen.UnsubscribeNotifications200JSONResponse{Category: string(cat)}, nil
}

// EmailProviderWebhook registra rebotes y quejas (NOT-007). La firma es el
// HMAC-SHA256 del cuerpo crudo con el secreto del proveedor.
func (h *NotificationHandlers) EmailProviderWebhook(ctx context.Context, req gen.EmailProviderWebhookRequestObject) (gen.EmailProviderWebhookResponseObject, error) {
	secret, ok := h.webhookSecrets[req.Provider]
	if !ok || len(secret) == 0 {
		return nil, &kernel.NotFoundError{Resource: "webhook"}
	}
	if !validSignature(secret, httpserver.RawBodyFrom(ctx), req.Params.XPPSignature) {
		return nil, kernel.NewDomainError("WEBHOOK_SIGNATURE_INVALID", "La firma del webhook no es válida.")
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	for _, ev := range req.Body.Events {
		if ev.Type != "bounce" && ev.Type != "complaint" {
			continue // tipos que no suprimen (entregado, abierto…) se ignoran
		}
		if err := h.supp.Suppress(ctx, app.AddressHash(ev.Address), ev.Type); err != nil {
			return nil, err
		}
	}
	return gen.EmailProviderWebhook204Response{}, nil
}

// validSignature compara en tiempo constante "sha256=<hex>" con el HMAC del cuerpo.
func validSignature(secret, body []byte, header string) bool {
	hexSig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || len(body) == 0 {
		return false
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hmac.Equal(got, m.Sum(nil))
}

func toDTO(p domain.Preferences) gen.NotificationPreferences {
	var out gen.NotificationPreferences
	out.Email.Appointments = p.Allows(domain.Appointments)
	out.Email.PreventiveCare = p.Allows(domain.PreventiveCare)
	out.Email.Clinical = p.Allows(domain.Clinical)
	out.Email.Account = p.Allows(domain.Account)
	out.Email.Security = true
	out.Digest.ClinicDailyAgenda = p.Digest.ClinicDailyAgenda
	return out
}

func fromDTO(d gen.NotificationPreferences) domain.Preferences {
	return domain.Preferences{
		Email: map[domain.Category]bool{
			domain.Appointments:   d.Email.Appointments,
			domain.PreventiveCare: d.Email.PreventiveCare,
			domain.Clinical:       d.Email.Clinical,
			domain.Account:        d.Email.Account,
			domain.Security:       true,
		},
		Digest: domain.Digest{ClinicDailyAgenda: d.Digest.ClinicDailyAgenda},
	}
}
