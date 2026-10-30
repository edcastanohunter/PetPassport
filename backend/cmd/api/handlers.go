package main

import (
	"github.com/edcastanohunter/petpassport/backend/api/gen"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	notificationshttp "github.com/edcastanohunter/petpassport/backend/internal/notifications/http"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	schedulinghttp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/http"
)

// apiHandlers compone el contrato completo a partir de los handlers de cada
// módulo (ADR-001: solo cmd/ conoce todos los módulos). Si una operación del
// contrato no está cubierta, la compilación falla aquí.
type apiHandlers struct {
	httpserver.SystemHandlers
	*identityhttp.Handlers
	*identityhttp.AdminHandlers
	*pethttp.PetHandlers
	*orghttp.OrgHandlers
	*clinicalhttp.ClinicalHandlers
	*schedulinghttp.SchedulingHandlers
	*notificationshttp.NotificationHandlers
}

var _ gen.StrictServerInterface = apiHandlers{}
