package httpserver

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// systemHandlers implementa las operaciones del tag `system` del contrato
// OpenAPI mediante el strict server generado (ADR-005, PP-F1-API-002).
type systemHandlers struct{}

var _ gen.StrictServerInterface = systemHandlers{}

// GetHealthz es la liveness pública (/api/v1/healthz).
func (systemHandlers) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse{Body: gen.HealthStatus{Status: gen.Ok}}, nil
}

// mountGenerated registra las rutas generadas bajo el router /api/v1 con los
// manejadores de error en formato problem+json.
func mountGenerated(api chi.Router) {
	strict := gen.NewStrictHandlerWithOptions(systemHandlers{}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			p := problem.New(problem.ValidationFailed, "Solicitud inválida", "La solicitud no cumple el contrato.")
			problem.Write(w, r, p)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			problem.Write(w, r, problem.New(problem.Internal, "Error interno", "Ocurrió un error inesperado."))
		},
	})
	gen.HandlerWithOptions(strict, gen.ChiServerOptions{
		BaseRouter: api,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Solicitud inválida", "Parámetros inválidos."))
		},
	})
}
