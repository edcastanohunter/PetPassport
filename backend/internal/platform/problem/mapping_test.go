package problem

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func TestFrom(t *testing.T) {
	ve := &kernel.ValidationError{}
	ve.Add("phone", "INVALID")
	cases := []struct {
		err    error
		status int
		code   Code
	}{
		{kernel.NewDomainError("CONSENT_VERSION_OUTDATED", "x"), http.StatusUnprocessableEntity, ConsentVersionOutdated},
		{kernel.NewDomainError("PET_DECEASED", "x"), http.StatusConflict, Code("PET_DECEASED")},
		{ve, http.StatusUnprocessableEntity, ValidationFailed},
		{&kernel.NotFoundError{Resource: "pet"}, http.StatusNotFound, NotFound},
		{&kernel.ForbiddenError{Reason: "r"}, http.StatusForbidden, Forbidden},
		{&kernel.ConcurrencyError{Expected: 1, Actual: 2}, http.StatusConflict, ConflictVersion},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, Unavailable},
		{errors.New("pg: boom"), http.StatusInternalServerError, Internal},
	}
	for _, tc := range cases {
		p := From(tc.err)
		if p.Status != tc.status || p.Code != tc.code {
			t.Errorf("From(%v) = %d %s, want %d %s", tc.err, p.Status, p.Code, tc.status, tc.code)
		}
		if p.Code == Internal && p.Detail != "Ocurrió un error inesperado." {
			t.Errorf("error interno filtra detalle: %q", p.Detail)
		}
	}
	if p := From(ve); len(p.Errors) != 1 || p.Errors[0].Field != "phone" {
		t.Fatalf("errors[] no mapeado: %+v", p.Errors)
	}
}
