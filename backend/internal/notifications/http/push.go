package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// WithPush inyecta el servicio de Web Push en los handlers.
func (h *NotificationHandlers) WithPush(push *app.PushService) *NotificationHandlers {
	h.push = push
	return h
}

// GetVAPIDPublicKey maneja GET /push/vapid-public-key (PP-F3-API-008).
func (h *NotificationHandlers) GetVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.PrincipalFrom(r.Context()); !ok {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	key := ""
	if h.push != nil {
		key = h.push.VAPIDPublicKey()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
}

// RegisterPushSubscription maneja POST /me/push-subscriptions (PP-F3-API-008).
func (h *NotificationHandlers) RegisterPushSubscription(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	var req app.PushSubscriptionInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "Cuerpo inválido", err.Error()))
		return
	}

	if h.push == nil {
		problem.Write(w, r, problem.New(problem.Internal, "Error interno", "Servicio push no inicializado"))
		return
	}

	id, err := h.push.RegisterSubscription(r.Context(), p.UserID, req)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}

// ListPushSubscriptions maneja GET /me/push-subscriptions (PP-F3-API-008).
func (h *NotificationHandlers) ListPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	if h.push == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}

	list, err := h.push.ListSubscriptions(r.Context(), p.UserID)
	if err != nil {
		problem.Write(w, r, problem.From(err))
		return
	}
	if list == nil {
		list = []app.PushSubscriptionView{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// DeletePushSubscription maneja DELETE /me/push-subscriptions/{id} (PP-F3-API-008).
func (h *NotificationHandlers) DeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r.Context())
	if err != nil {
		problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere token"))
		return
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		problem.Write(w, r, problem.New(problem.Forbidden, "Acceso denegado", d.Reason))
		return
	}

	subID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.Write(w, r, problem.New(problem.ValidationFailed, "ID inválido", "El formato del id debe ser UUID"))
		return
	}

	if h.push != nil {
		_ = h.push.DeleteSubscription(r.Context(), p.UserID, subID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// MountPushRoutes monta las rutas de Web Push en el router chi (PP-F3-API-008).
func (h *NotificationHandlers) MountPushRoutes(r chi.Router) {
	r.Get("/push/vapid-public-key", h.GetVAPIDPublicKey)
	r.Post("/me/push-subscriptions", h.RegisterPushSubscription)
	r.Get("/me/push-subscriptions", h.ListPushSubscriptions)
	r.Delete("/me/push-subscriptions/{id}", h.DeletePushSubscription)
}
