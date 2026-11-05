package http

import (
	"github.com/go-chi/chi/v5"
)

// MountRoutes registra todas las rutas de medicamentos en el enrutador chi (PP-F3-API-003).
func (h *MedicationHandler) MountRoutes(r chi.Router) {
	// Catálogo de consulta clínica
	r.Route("/catalogs/medications", func(r chi.Router) {
		r.Get("/", h.ListMedications)
		r.Get("/{id}", h.GetMedication)
		r.Post("/suggestions", h.CreateSuggestion)
	})

	// Sugerencias dentro del espacio de organización
	r.Route("/organizations/{org_id}/catalogs/medications", func(r chi.Router) {
		r.Get("/suggestions", h.ListOrgSuggestions)
	})

	// Autocompletado rápido
	r.Get("/suggest/medications", h.SuggestMedications)

	// Administración del catálogo (catalog:admin / super_admin)
	r.Route("/admin/catalogs/medications", func(r chi.Router) {
		r.Post("/", h.AdminCreateMedication)
		r.Put("/{id}", h.AdminUpdateMedication)
		r.Post("/{id}/discontinue", h.AdminDiscontinueMedication)
		r.Post("/{id}/merge", h.AdminMergeMedication)
		r.Post("/import", h.AdminImport)
		r.Get("/imports/{batch_id}", h.AdminGetImport)
		r.Post("/imports/{batch_id}/revert", h.AdminRevertImport)
		r.Get("/suggestions", h.AdminListSuggestions)
		r.Post("/suggestions/{id}/approve", h.AdminApproveSuggestion)
		r.Post("/suggestions/{id}/reject", h.AdminRejectSuggestion)
		r.Get("/usage", h.AdminUsageStats)
	})
}
