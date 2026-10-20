package httpserver

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// requestValidator valida parámetros y cuerpo contra el contrato antes del
// handler (PP-F1-API-010): additionalProperties, enum, pattern, maxLength,
// Content-Type. Los mensajes no reflejan el valor recibido.
func requestValidator(spec *openapi3.T) func(http.Handler) http.Handler {
	// Copia superficial: el router interno no debe validar el host de `servers`.
	s := *spec
	s.Servers = nil
	return nethttpmiddleware.OapiRequestValidatorWithOptions(&s, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc:         openapi3filter.NoopAuthenticationFunc, // la autenticación la hace el guard
			ExcludeReadOnlyValidations: true,
		},
		DoNotValidateServers:  true,
		SilenceServersWarning: true,
		Prefix:                "/api/v1",
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, _ nethttpmiddleware.ErrorHandlerOpts) {
			problem.Write(w, r, validationProblem(err))
		},
	})
}

var propertyRe = regexp.MustCompile(`property "([A-Za-z0-9_.-]{1,64})"`)

func validationProblem(err error) *problem.Problem {
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) && reqErr.RequestBody != nil && strings.Contains(reqErr.Reason, "Content-Type") {
		return problem.New(problem.UnsupportedMedia, "Tipo de contenido no soportado", "Usa application/json.")
	}
	p := problem.New(problem.ValidationFailed, "Solicitud inválida", "La solicitud no cumple el contrato.")
	for _, se := range schemaErrors(err) {
		field := strings.Join(se.JSONPointer(), ".")
		code := codeFor(se.SchemaField)
		// Para `required` y `additionalProperties` el puntero apunta al objeto;
		// el nombre de la propiedad (no su valor) va en Reason.
		if m := propertyRe.FindStringSubmatch(se.Reason); m != nil {
			if field != "" {
				field += "."
			}
			field += m[1]
			switch {
			case strings.Contains(se.Reason, "is missing"):
				code = "REQUIRED"
			case strings.Contains(se.Reason, "is unsupported"):
				code = "UNKNOWN_FIELD"
			}
		}
		if field == "" {
			field = "body"
		}
		p.Errors = append(p.Errors, problem.FieldError{Field: field, Code: code})
	}
	if reqErr != nil && reqErr.Parameter != nil && len(p.Errors) == 0 {
		p.Errors = append(p.Errors, problem.FieldError{Field: reqErr.Parameter.Name, Code: "INVALID"})
	}
	return p
}

func schemaErrors(err error) []*openapi3.SchemaError {
	var out []*openapi3.SchemaError
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, e := range multi {
			out = append(out, schemaErrors(e)...)
		}
		return out
	}
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		if se.Origin != nil {
			if inner := schemaErrors(se.Origin); len(inner) > 0 {
				return inner
			}
		}
		out = append(out, se)
	}
	return out
}

func codeFor(schemaField string) string {
	switch schemaField {
	case "required":
		return "REQUIRED"
	case "enum":
		return "INVALID_ENUM"
	case "maxLength", "maxItems", "maximum":
		return "TOO_LONG"
	case "minLength", "minItems", "minimum", "minProperties":
		return "TOO_SHORT"
	case "pattern", "format":
		return "INVALID_FORMAT"
	case "additionalProperties":
		return "UNKNOWN_FIELD"
	case "type":
		return "INVALID_TYPE"
	}
	return "INVALID"
}
