package main

import (
	"github.com/edcastanohunter/petpassport/backend/api/gen"
	billinghttp "github.com/edcastanohunter/petpassport/backend/internal/billing/http"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	notificationshttp "github.com/edcastanohunter/petpassport/backend/internal/notifications/http"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	pharmacyhttp "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	prescriptionhttp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/http"
	schedulinghttp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/http"
)

// apiHandlers compone el contrato completo a partir de los handlers de cada
// módulo (ADR-001: solo cmd/ conoce todos los módulos). Si una operación del
// contrato no está cubierta, la compilación falla aquí.
type apiHandlers struct {
	httpserver.SystemHandlers
	*identityhttp.Handlers
	*identityhttp.AdminHandlers
	*identityhttp.CredentialHandlers
	*pethttp.PetHandlers
	*orghttp.OrgHandlers
	*orghttp.StatsHandlers
	*clinicalhttp.ClinicalHandlers
	*schedulinghttp.SchedulingHandlers
	*notificationshttp.NotificationHandlers
	*prescriptionhttp.PrescriptionHandlers
	*pharmacyhttp.PharmacyHandlers
	*billinghttp.BillingHandlers
}

var _ gen.StrictServerInterface = apiHandlers{}
