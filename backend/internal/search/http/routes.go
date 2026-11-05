package http

import (
	"github.com/go-chi/chi/v5"
)

// MountRoutes registra las rutas del módulo de búsqueda en el router de Chi.
func (h *SearchHandler) MountRoutes(r chi.Router) {
	// Espacio de organizaciones
	r.Route("/organizations/{org_id}", func(r chi.Router) {
		r.Get("/search/patients", h.SearchPatients)
		r.Get("/search/clinical", h.SearchClinical)
		r.Get("/suggest/patients", h.SuggestPatients)

		// Cohortes (PP-F3-API-004)
		r.Get("/cohorts", h.GetCohorts)
		r.Post("/cohorts/{key}/run", h.RunCohort)
		r.Post("/cohorts/{key}/export", h.ExportCohort)
		r.Post("/cohorts/{key}/notify", h.NotifyCohort)
	})

	// Espacio del tutor autenticado
	r.Route("/me", func(r chi.Router) {
		r.Get("/search", h.SearchMe)
		r.Get("/suggest/pets", h.SuggestPetsMe)
	})

	// Espacio de administración
	r.Route("/admin", func(r chi.Router) {
		r.Get("/search/clinical", h.AdminSearchClinical)
		r.Get("/search/indices", h.ListIndices)
		r.Post("/search/indices/{alias}/reindex", h.ReindexIndex)
		r.Post("/search/index-queue/retry", h.RetryQueue)
		r.Post("/cache/purge", h.PurgeCache)

		// Panel de plataforma (PP-F3-API-006)
		r.Get("/platform/stats", h.GetPlatformStats)
		r.Get("/platform/top-organizations", h.GetTopOrganizations)
	})
}
