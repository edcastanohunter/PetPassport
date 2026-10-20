package main

import (
	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
)

// apiHandlers compone el contrato completo a partir de los handlers de cada
// módulo (ADR-001: solo cmd/ conoce todos los módulos). Si una operación del
// contrato no está cubierta, la compilación falla aquí.
type apiHandlers struct {
	httpserver.SystemHandlers
	*identityhttp.Handlers
}

var _ gen.StrictServerInterface = apiHandlers{}
