package httpserver

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// SystemHandlers implementa las operaciones del tag `system` del contrato
// OpenAPI (ADR-005, PP-F1-API-002). Los módulos de negocio aportan el resto y
// cmd/api los compone en un único gen.StrictServerInterface.
type SystemHandlers struct{}

// GetHealthz es la liveness pública (/api/v1/healthz).
func (SystemHandlers) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse{Body: gen.HealthStatus{Status: gen.Ok}}, nil
}

// systemOnly cubre el contrato solo con las operaciones de sistema (pruebas de
// la capa HTTP). Las demás operaciones no se alcanzan: sin guard responden 503.
type systemOnly struct {
	gen.StrictServerInterface
	SystemHandlers
}

func (systemOnly) GetHealthz(ctx context.Context, r gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return SystemHandlers{}.GetHealthz(ctx, r)
}

// mountGenerated registra las rutas generadas bajo el router /api/v1 con los
// manejadores de error en formato problem+json. Los middlewares por operación
// (captura de cuerpo, guard, validación) se aplican tras el enrutado, de modo
// que chi ya conoce el patrón de ruta cuando el guard lo consulta. perOperation
// se da en orden de ejecución (el primero es el más externo).
func mountGenerated(api chi.Router, handlers gen.StrictServerInterface, perOperation []func(http.Handler) http.Handler) {
	strict := gen.NewStrictHandlerWithOptions(handlers, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			p := problem.New(problem.ValidationFailed, "Solicitud inválida", "La solicitud no cumple el contrato.")
			problem.Write(w, r, p)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			problem.Write(w, r, problem.From(err))
		},
	})
	// El código generado envuelve en orden de lista (el último queda más externo).
	mws := make([]gen.MiddlewareFunc, 0, len(perOperation))
	for i := len(perOperation) - 1; i >= 0; i-- {
		mws = append(mws, perOperation[i])
	}
	gen.HandlerWithOptions(strict, gen.ChiServerOptions{
		BaseRouter:  api,
		Middlewares: mws,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			problem.Write(w, r, problem.New(problem.ValidationFailed, "Solicitud inválida", "Parámetros inválidos."))
		},
	})
}
