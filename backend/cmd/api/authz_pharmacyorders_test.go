package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	pharmacyapp "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	pharmacydomain "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	pharmacyhttp "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/http"
	pharmacyinfra "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

type fakePharmacyQueries struct{}

func (fakePharmacyQueries) GetQuoteRequest(_ context.Context, id uuid.UUID) (*pharmacyinfra.QuoteRequestDetailView, error) {
	now := time.Now()
	exp := now.Add(24 * time.Hour)
	return &pharmacyinfra.QuoteRequestDetailView{
		ID:             id,
		PrescriptionID: rxID,
		GuardianID:     tutorID,
		Status:         "open",
		Mode:           "delivery",
		Items: []pharmacydomain.RequestedItem{
			{
				ItemID:       "item-1",
				NameSnapshot: "Amoxicilina",
				Quantity:     1,
				Unit:         "box",
			},
		},
		CreatedAt: now,
		ExpiresAt: exp,
	}, nil
}

func (fakePharmacyQueries) ListGuardianQuoteRequests(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]pharmacyinfra.QuoteRequestDetailView, error) {
	now := time.Now()
	return []pharmacyinfra.QuoteRequestDetailView{
		{
			ID:             rxID,
			PrescriptionID: rxID,
			GuardianID:     tutorID,
			Status:         "open",
			Mode:           "delivery",
			CreatedAt:      now,
			ExpiresAt:      now.Add(24 * time.Hour),
		},
	}, nil
}

func (fakePharmacyQueries) GetOrder(_ context.Context, id uuid.UUID) (*pharmacyinfra.OrderDetailView, error) {
	now := time.Now()
	return &pharmacyinfra.OrderDetailView{
		ID:             id,
		QuoteRequestID: rxID,
		PrescriptionID: rxID,
		GuardianID:     tutorID,
		PharmacyOrgID:  orgActive,
		PharmacyName:   "Dev Pharmacy",
		PharmacyPhone:  "+573001234567",
		Status:         "placed",
		Mode:           "delivery",
		Totals: pharmacydomain.OrderTotals{
			Total:       25.0,
			DeliveryFee: 5.0,
			Subtotal:    20.0,
			Currency:    "COP",
		},
		Lines: []pharmacydomain.OrderLine{
			{
				ItemID:    "item-1",
				Qty:       1,
				UnitPrice: 20.0,
			},
		},
		Timeline: []pharmacydomain.OrderTimelineEntry{
			{Status: "placed", At: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (fakePharmacyQueries) ListGuardianOrders(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]pharmacyinfra.OrderDetailView, error) {
	now := time.Now()
	return []pharmacyinfra.OrderDetailView{
		{
			ID:             rxID,
			QuoteRequestID: rxID,
			PrescriptionID: rxID,
			GuardianID:     tutorID,
			PharmacyOrgID:  orgActive,
			PharmacyName:   "Dev Pharmacy",
			Status:         "placed",
			Mode:           "delivery",
			Totals:         pharmacydomain.OrderTotals{Total: 25.0},
			CreatedAt:      now,
		},
	}, nil
}

func (fakePharmacyQueries) ListPharmacyInbox(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]pharmacyinfra.PharmacyInboxItem, error) {
	now := time.Now()
	return []pharmacyinfra.PharmacyInboxItem{
		{
			QuoteRequestID:   rxID,
			ReceivedAt:       now,
			ExpiresAt:        now.Add(24 * time.Hour),
			InvitationStatus: "pending",
			Pet:              pharmacydomain.AnimalSnapshot{Species: "canine"},
			Mode:             "delivery",
			Prescription:     pharmacyinfra.InboxPrescriptionRef{ID: rxID},
		},
	}, nil
}

func (fakePharmacyQueries) ListPharmacyOrders(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]pharmacyinfra.OrderDetailView, error) {
	now := time.Now()
	return []pharmacyinfra.OrderDetailView{
		{
			ID:             rxID,
			QuoteRequestID: rxID,
			PrescriptionID: rxID,
			GuardianID:     tutorID,
			PharmacyOrgID:  orgActive,
			Status:         "placed",
			Mode:           "delivery",
			Totals:         pharmacydomain.OrderTotals{Total: 25.0},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}, nil
}

func (fakePharmacyQueries) GetPrescriptionQuoteOptions(_ context.Context, id uuid.UUID) (*pharmacyinfra.QuoteOptionsView, error) {
	return &pharmacyinfra.QuoteOptionsView{
		PrescriptionID: id,
		GuardianID:     tutorID,
		Items: []pharmacyinfra.QuoteOptionItem{
			{
				PrescriptionItemID: rxID,
				MedicationName:     "Amoxicilina",
				Dose:               "250mg",
				Quantity:           1,
				Unit:               "box",
				IsControlled:       false,
			},
		},
		Pharmacies: []pharmacyinfra.EligiblePharmacy{
			{
				PharmacyID:              orgActive,
				Name:                    "Farmacia Aliada",
				Address:                 "Calle 10 # 20-30",
				Rating:                  4.8,
				Active:                  true,
				FulfillmentTypes:        []string{"delivery", "pickup"},
				AverageQuoteTimeSeconds: 600,
				OnTimeDeliveryRate:      0.98,
			},
		},
	}, nil
}

func (fakePharmacyQueries) GetIntegrationSettings(_ context.Context, _ uuid.UUID) (*pharmacyinfra.PharmacyIntegrationSettingsView, error) {
	return &pharmacyinfra.PharmacyIntegrationSettingsView{
		Level:                 "portal",
		CatalogMappingEnabled: true,
		DeliveryAreas: []pharmacyinfra.DeliveryAreaView{
			{City: "Bogota", PostalCodes: []string{"110111"}},
		},
	}, nil
}

func (fakePharmacyQueries) UpdateIntegrationSettings(_ context.Context, _ uuid.UUID, _ string, _ *string, _ bool, _ []pharmacyinfra.DeliveryAreaView) error {
	return nil
}

func (fakePharmacyQueries) ListProductMappings(_ context.Context, _ uuid.UUID, _, _ int) ([]pharmacyinfra.ProductMappingItemView, int, error) {
	now := time.Now()
	return []pharmacyinfra.ProductMappingItemView{
		{
			CatalogID:    rxID,
			StandardName: "Amoxicilina 250mg",
			PharmacySKU:  "SKU-1001",
			PharmacyName: "Amoxil 250",
			PriceCents:   1500,
			InStock:      true,
			UpdatedAt:    now,
		},
	}, 1, nil
}

func (fakePharmacyQueries) UpsertProductMapping(_ context.Context, _, _ uuid.UUID, _ string, _ string, _ int, _ bool) error {
	return nil
}

func (fakePharmacyQueries) DeleteProductMapping(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

type fakePharmacyQRRepo struct{}

func (fakePharmacyQRRepo) Load(_ context.Context, id uuid.UUID) (*pharmacydomain.QuoteRequest, error) {
	qr := pharmacydomain.NewQuoteRequest()
	qr.ID = id
	qr.GuardianID = tutorID
	qr.PrescriptionID = rxID
	qr.Status = pharmacydomain.QuoteRequestStatusOpen
	qr.ExpiresAt = time.Now().Add(24 * time.Hour)
	qr.Invited[orgActive] = pharmacydomain.Invitation{OrgID: orgActive, Status: pharmacydomain.InvitationPending}
	return qr, nil
}

func (fakePharmacyQRRepo) Save(_ context.Context, _ *pharmacydomain.QuoteRequest) ([]eventstore.StoredEvent, error) {
	return nil, nil
}

type fakePharmacyOrderRepo struct{}

func (fakePharmacyOrderRepo) Load(_ context.Context, id uuid.UUID) (*pharmacydomain.Order, error) {
	o := pharmacydomain.NewOrder()
	o.ID = id
	o.QuoteRequestID = rxID
	o.PrescriptionID = rxID
	o.GuardianID = tutorID
	o.PharmacyOrgID = orgActive
	o.Status = pharmacydomain.OrderStatusPlaced
	return o, nil
}

func (fakePharmacyOrderRepo) Save(_ context.Context, _ *pharmacydomain.Order) ([]eventstore.StoredEvent, error) {
	return nil, nil
}

type fakeUoW struct{}

func (fakeUoW) Do(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeProjector struct{}

func (fakeProjector) Apply(_ context.Context, _ []eventstore.StoredEvent) error {
	return nil
}

type fakeGateway struct{}

func (fakeGateway) SendQuoteRequest(_ context.Context, _ uuid.UUID, _ pharmacydomain.QuoteRequestPayload) (pharmacydomain.SendResult, error) {
	return pharmacydomain.SendResult{Channel: "portal", SentAt: time.Now()}, nil
}

func (fakeGateway) NotifyOrder(_ context.Context, _ uuid.UUID, _ pharmacydomain.OrderPayload) (pharmacydomain.SendResult, error) {
	return pharmacydomain.SendResult{Channel: "portal", SentAt: time.Now()}, nil
}

func (fakeGateway) CancelOrder(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) error {
	return nil
}

func (fakeGateway) Capabilities(_ context.Context, _ uuid.UUID) (pharmacydomain.PartnerCapabilities, error) {
	return pharmacydomain.PartnerCapabilities{Delivery: true, Pickup: true, Substitutions: true}, nil
}

type fakeRxReader struct{}

func (fakeRxReader) GetPrescription(_ context.Context, id uuid.UUID) (*pharmacydomain.PrescriptionSnapshot, error) {
	return &pharmacydomain.PrescriptionSnapshot{
		ID:         id,
		GuardianID: tutorID,
		Status:     "issued",
		Country:    "CO",
		Items: []pharmacydomain.PrescriptionItemSnapshot{
			{
				ItemID:   "item-1",
				Name:     "Amoxicilina",
				Quantity: 1,
				Unit:     "box",
			},
		},
	}, nil
}

type fakeQuotaChecker struct{}

func (fakeQuotaChecker) CheckQuotas(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ time.Time) error {
	return nil
}

type fakePartnerDirectory struct{}

func (fakePartnerDirectory) EligiblePharmacies(_ context.Context, _ string, _ pharmacydomain.Area, _ pharmacydomain.FulfillmentMode) ([]pharmacydomain.EligiblePharmacy, error) {
	return []pharmacydomain.EligiblePharmacy{
		{
			OrganizationID: orgActive,
			DisplayName:    "Dev Pharmacy",
			City:           "Bogota",
			Modes:          []pharmacydomain.FulfillmentMode{"delivery", "pickup"},
			Capabilities:   pharmacydomain.PartnerCapabilities{Delivery: true, Pickup: true},
		},
	}, nil
}

func (fakePartnerDirectory) IsEligible(_ context.Context, _ uuid.UUID, _ pharmacydomain.Area, _ pharmacydomain.FulfillmentMode) (bool, error) {
	return true, nil
}

type fakeEquivalence struct{}

func (fakeEquivalence) IsControlled(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (fakeEquivalence) AreEquivalent(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

type fakeDispenser struct{}

func (fakeDispenser) RecordDispensation(_ context.Context, _, _, _ uuid.UUID, _ []pharmacydomain.DispensationLine) error {
	return nil
}

type fakePetSnapshotReader struct{}

func (fakePetSnapshotReader) GetAnimalSnapshot(_ context.Context, _ uuid.UUID) (pharmacydomain.AnimalSnapshot, error) {
	return pharmacydomain.AnimalSnapshot{Species: "canine"}, nil
}

type fakeUserContactReader struct{}

func (fakeUserContactReader) GetUserName(_ context.Context, _ uuid.UUID) (string, error) {
	return "Tutor Name", nil
}

func (fakeUserContactReader) GetUserEmail(_ context.Context, _ uuid.UUID) (string, error) {
	return "tutor@example.com", nil
}

func newSuitePharmacyHandlers(logger *slog.Logger) *pharmacyhttp.PharmacyHandlers {
	svc := pharmacyapp.NewService(pharmacyapp.Options{
		QuoteRequests:         fakePharmacyQRRepo{},
		Orders:                fakePharmacyOrderRepo{},
		UoW:                   fakeUoW{},
		Projects:              fakeProjector{},
		Gateway:               fakeGateway{},
		Prescriptions:         fakeRxReader{},
		PrescriptionDispenser: fakeDispenser{},
		Catalogs:              fakeEquivalence{},
		Directory:             fakePartnerDirectory{},
		QuotaChecker:          fakeQuotaChecker{},
		PetReader:             fakePetSnapshotReader{},
		UserReader:            fakeUserContactReader{},
		Logger:                logger,
	})
	return pharmacyhttp.NewPharmacyHandlers(svc, fakePharmacyQueries{}, logger)
}

// pharmacyMatrix: columnas = tutor vetL admL recL vetU vetS phAd str sup.
var pharmacyMatrix = map[string][9]int{
	// quote:request (solo tutor)
	"createQuoteRequest": {ok, 403, 403, 403, 403, 403, 403, 403, 403},
	"selectQuote":        {ok, 403, 403, 403, 403, 403, 403, 403, 403},
	"cancelQuoteRequest": {ok, 403, 403, 403, 403, 403, 403, 403, 403},

	// quote:read (tutor o super_admin)
	"getPrescriptionQuoteOptions": {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"listQuoteRequests":           {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"getQuoteRequest":             {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"streamQuoteRequestEvents":    {ok, 403, 403, 403, 403, 403, 403, 403, ok},

	// order:read (tutor o super_admin)
	"listOrders":        {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"getOrder":          {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"streamOrderEvents": {ok, 403, 403, 403, 403, 403, 403, 403, ok},

	// order:cancel (tutor o super_admin)
	"cancelOrder": {ok, 403, 403, 403, 403, 403, 403, 403, ok},

	// pharmacy:orders (personal de farmacia activa)
	"getPharmacyInbox":                {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"listPharmacyOrders":              {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"getPharmacyOrder":                {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"confirmPharmacyOrder":            {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"rejectPharmacyOrder":             {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"markPharmacyOrderPreparing":      {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"markPharmacyOrderReady":          {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"markPharmacyOrderOutForDelivery": {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"deliverPharmacyOrder":            {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"cancelPharmacyOrder":             {403, 403, 403, 403, 403, 403, ok, 403, 403},

	// pharmacy:quote (personal de farmacia activa)
	"submitPharmacyQuote":         {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"declinePharmacyQuoteRequest": {403, 403, 403, 403, 403, 403, ok, 403, 403},

	// pharmacy:validate (personal de farmacia activa)
	"validatePharmacyPrescription": {403, 403, 403, 403, 403, 403, ok, 403, 403},

	// pharmacy:admin (administrador de farmacia activa)
	"getPharmacyIntegration":           {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"updatePharmacyIntegration":        {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"listPharmacyProductMappings":      {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"upsertPharmacyProductMapping":     {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"deletePharmacyProductMapping":     {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"importPharmacyProductMappings":    {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"getPharmacyApiCredentials":        {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"rotatePharmacyApiCredentials":     {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"createPharmacySandboxCredentials": {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"getPharmacyWebhook":               {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"updatePharmacyWebhook":            {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"testPharmacyWebhook":              {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"listPharmacyWebhookDeliveries":    {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"retryPharmacyWebhookDelivery":     {403, 403, 403, 403, 403, 403, ok, 403, 403},
	"getPharmacyStats":                 {403, 403, 403, 403, 403, 403, ok, 403, 403},
}

func init() {
	for k, v := range pharmacyMatrix {
		matrix[k] = v
	}
}
