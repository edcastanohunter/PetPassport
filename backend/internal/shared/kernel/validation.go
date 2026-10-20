package kernel

import "strings"

// FieldViolation es un error de validación por campo (API-011 `errors[]`).
// El código nunca refleja el valor recibido (API-010).
type FieldViolation struct {
	Field string
	Code  string
}

// ValidationError agrupa violaciones de validación semántica (VOs) que el
// esquema OpenAPI no puede expresar. Se mapea a 422 VALIDATION_FAILED.
type ValidationError struct {
	Fields []FieldViolation
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+":"+f.Code)
	}
	return "validation failed: " + strings.Join(parts, ", ")
}

// Add registra una violación.
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldViolation{Field: field, Code: code})
}

// Any indica si hay violaciones.
func (e *ValidationError) Any() bool { return len(e.Fields) > 0 }

// ErrOrNil devuelve el error si hay violaciones; nil en caso contrario.
func (e *ValidationError) ErrOrNil() error {
	if e.Any() {
		return e
	}
	return nil
}

// ForbiddenError indica que la política authz deniega la acción sobre un
// recurso visible (ARQ-010 familia app.Error).
type ForbiddenError struct{ Reason string }

func (e *ForbiddenError) Error() string { return "forbidden: " + e.Reason }
