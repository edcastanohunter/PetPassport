package problem

import (
	"context"
	"errors"
	"net/http"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// From mapea las tres familias de errores de ARQ-010 a un Problem:
// DomainError → código del catálogo; NotFoundError → 404; ForbiddenError → 403;
// ValidationError → 422 con errors[]; cualquier otro → 500 sin detalles internos.
func From(err error) *Problem {
	var de *kernel.DomainError
	var nf *kernel.NotFoundError
	var fb *kernel.ForbiddenError
	var ve *kernel.ValidationError
	var ce *kernel.ConcurrencyError
	switch {
	case errors.As(err, &de):
		p := New(Code(de.Code), titleFor(Code(de.Code)), de.Message)
		if _, known := defaultStatus[Code(de.Code)]; !known {
			p.Status = http.StatusConflict
		}
		return p
	case errors.As(err, &ve):
		p := New(ValidationFailed, "Datos inválidos", "Uno o más campos no son válidos.")
		for _, f := range ve.Fields {
			p.Errors = append(p.Errors, FieldError{Field: f.Field, Code: f.Code})
		}
		return p
	case errors.As(err, &nf):
		return New(NotFound, "No encontrado", "El recurso no existe o no es visible.")
	case errors.As(err, &fb):
		return New(Forbidden, "Prohibido", "No tienes permiso para esta acción.")
	case errors.As(err, &ce):
		return New(ConflictVersion, "Conflicto de versión", "El recurso cambió; recarga y reintenta.")
	case errors.Is(err, context.DeadlineExceeded):
		return New(Unavailable, "No disponible", "La operación tardó demasiado.")
	}
	return New(Internal, "Error interno", "Ocurrió un error inesperado.")
}

var titles = map[Code]string{
	ConsentVersionOutdated: "Consentimiento no vigente",
	AccountSuspended:       "Cuenta suspendida",
	OnboardingRequired:     "Onboarding pendiente",
	EmailNotVerified:       "Email no verificado",
}

func titleFor(c Code) string {
	if t, ok := titles[c]; ok {
		return t
	}
	return "Operación no permitida"
}
