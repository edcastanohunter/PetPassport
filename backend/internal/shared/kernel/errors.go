package kernel

import "fmt"

// DomainError es un error de regla de negocio con código estable del catálogo
// (docs/phase-1/03 §7). Se mapea a problem+json en la capa http.
type DomainError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *DomainError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// NewDomainError construye un DomainError.
func NewDomainError(code, message string) *DomainError {
	return &DomainError{Code: code, Message: message}
}

// NotFoundError indica que el recurso no existe o no es visible para el actor.
type NotFoundError struct{ Resource string }

func (e *NotFoundError) Error() string { return e.Resource + " not found" }

// ConcurrencyError indica un conflicto de versión al persistir un agregado.
type ConcurrencyError struct {
	Expected, Actual int64
}

func (e *ConcurrencyError) Error() string {
	return fmt.Sprintf("concurrency conflict: expected version %d, actual %d", e.Expected, e.Actual)
}
