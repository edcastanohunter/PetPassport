package testkit

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
)

// Contract valida requests/responses contra el contrato OpenAPI (PP-F1-QA-004).
type Contract struct {
	t      testing.TB
	router routers.Router
}

// NewContract carga el contrato embebido.
func NewContract(t testing.TB) *Contract {
	t.Helper()
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	// Los servidores del contrato tienen prefijo /api/v1; para enrutar peticiones
	// de httptest se sustituyen por uno relativo.
	spec.Servers = openapi3.Servers{{URL: "/api/v1"}}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatal(err)
	}
	return &Contract{t: t, router: router}
}

// Do ejecuta el request contra el handler y valida la respuesta contra el
// contrato (estado, content-type y esquema). Devuelve el recorder.
func (c *Contract) Do(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	c.t.Helper()
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	route, pathParams, err := c.router.FindRoute(req)
	if err != nil {
		c.t.Fatalf("ruta %s %s no está en el contrato: %v", req.Method, req.URL.Path, err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	reqInput := &openapi3filter.RequestValidationInput{
		Request: req, PathParams: pathParams, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput, Status: rec.Code, Header: rec.Header(),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	respInput.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), respInput); err != nil {
		c.t.Fatalf("respuesta %s %s (%d) no cumple el contrato: %v\n%s", req.Method, req.URL.Path, rec.Code, err, rec.Body.String())
	}
	return rec
}
