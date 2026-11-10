package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries encapsula las consultas SQL optimizadas a los read models de cotización y pedidos (ARQ-014, DAT-002).
type Queries struct {
	pool *pgxpool.Pool
}

// NewQueries construye el servicio de lecturas.
func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

// QuoteRequestDetailView modela la vista completa de la solicitud para el tablero del tutor (FE-005).
type QuoteRequestDetailView struct {
	ID              uuid.UUID                 `json:"id"`
	Version         int64                     `json:"version"`
	PrescriptionID  uuid.UUID                 `json:"prescription_id"`
	PetID           uuid.UUID                 `json:"pet_id"`
	GuardianID      uuid.UUID                 `json:"guardian_id"`
	Country         string                    `json:"country"`
	Items           []domain.RequestedItem    `json:"items"`
	Mode            domain.FulfillmentMode    `json:"mode"`
	Area            domain.Area               `json:"area"`
	Status          domain.QuoteRequestStatus `json:"status"`
	ExpiresAt       time.Time                 `json:"expires_at"`
	SelectableUntil *time.Time                `json:"selectable_until,omitempty"`
	SelectedQuoteID *uuid.UUID                `json:"selected_quote_id,omitempty"`
	OrderID         *uuid.UUID                `json:"order_id,omitempty"`
	ClosedReason    *string                   `json:"closed_reason,omitempty"`
	Invitations     []domain.Invitation       `json:"invitations"`
	Quotes          []domain.Quote            `json:"quotes"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

// OrderDetailView modela la vista detallada de un pedido para el tutor o la farmacia (FE-006, FE-008).
type OrderDetailView struct {
	ID              uuid.UUID                   `json:"id"`
	Version         int64                       `json:"version"`
	QuoteRequestID  uuid.UUID                   `json:"quote_request_id"`
	QuoteID         uuid.UUID                   `json:"quote_id"`
	PrescriptionID  uuid.UUID                   `json:"prescription_id"`
	PetID           uuid.UUID                   `json:"pet_id"`
	GuardianID      uuid.UUID                   `json:"guardian_id"`
	PharmacyOrgID   uuid.UUID                   `json:"pharmacy_org_id"`
	PharmacyName    string                      `json:"pharmacy_name"`
	PharmacyPhone   string                      `json:"pharmacy_phone"`
	PharmacyCity    string                      `json:"pharmacy_city"`
	Mode            domain.FulfillmentMode      `json:"mode"`
	DeliveryAddress *domain.Address             `json:"delivery_address,omitempty"`
	ContactPhone    string                      `json:"contact_phone"`
	Lines           []domain.OrderLine          `json:"lines"`
	Totals          domain.OrderTotals          `json:"totals"`
	Status          domain.OrderStatus          `json:"status"`
	Timeline        []domain.OrderTimelineEntry `json:"timeline"`
	Dispensation    *domain.DispensationRecord  `json:"dispensation,omitempty"`
	PartnerRef      *string                     `json:"partner_ref,omitempty"`
	Rating          *OrderRatingView            `json:"rating,omitempty"`
	PaymentNotice   string                      `json:"payment_notice"`
	CreatedAt       time.Time                   `json:"created_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
}

// OrderRatingView modela la valoración del pedido (PP-F4-PRD-022).
type OrderRatingView struct {
	Score   int       `json:"score"`
	Comment string    `json:"comment,omitempty"`
	RatedAt time.Time `json:"rated_at"`
}

// PharmacyInboxItem modela una solicitud recibida en la bandeja del portal de farmacia (FE-007, API-007).
type PharmacyInboxItem struct {
	QuoteRequestID   uuid.UUID               `json:"quote_request_id"`
	ReceivedAt       time.Time               `json:"received_at"`
	ExpiresAt        time.Time               `json:"expires_at"`
	InvitationStatus domain.InvitationStatus `json:"invitation_status"`
	Pet              domain.AnimalSnapshot   `json:"pet"`
	Items            []InboxItemLine         `json:"items"`
	Mode             domain.FulfillmentMode  `json:"mode"`
	Area             domain.Area             `json:"area"`
	Prescription     InboxPrescriptionRef    `json:"prescription"`
}

// InboxItemLine representa un ítem de receta con mapeo de SKU propio si existe (FE-007).
type InboxItemLine struct {
	ItemID              string              `json:"item_id"`
	CatalogID           *uuid.UUID          `json:"catalog_id,omitempty"`
	Name                string              `json:"name"`
	Form                string              `json:"form,omitempty"`
	Concentration       string              `json:"concentration,omitempty"`
	FreeText            *string             `json:"free_text,omitempty"`
	Dose                string              `json:"dose"`
	Frequency           string              `json:"frequency"`
	Quantity            float64             `json:"quantity"`
	Unit                string              `json:"unit"`
	SubstitutionAllowed bool                `json:"substitution_allowed"`
	MyMapping           *ProductMappingView `json:"my_mapping,omitempty"`
}

// ProductMappingView describe el SKU y precio base configurado por la farmacia (DOM-011).
type ProductMappingView struct {
	CatalogID   uuid.UUID `json:"catalog_id"`
	PartnerSKU  string    `json:"partner_sku"`
	PartnerName string    `json:"partner_name,omitempty"`
	BasePrice   *float64  `json:"base_price,omitempty"`
	Active      bool      `json:"active"`
}

// InboxPrescriptionRef referencia los detalles de la receta visible para la farmacia (ARQ-013).
type InboxPrescriptionRef struct {
	ID        uuid.UUID `json:"id"`
	Source    string    `json:"source"`
	Verified  bool      `json:"verified"`
	VerifyURL *string   `json:"verify_url,omitempty"`
	ImageURLs []string  `json:"image_urls,omitempty"`
}

// GetQuoteRequest consulta el detalle completo de una cotización para el tutor.
func (q *Queries) GetQuoteRequest(ctx context.Context, id uuid.UUID) (*QuoteRequestDetailView, error) {
	var (
		version                  int64
		rxID, petID, guardianID  uuid.UUID
		country, mode, status    string
		itemsJSON, areaJSON      []byte
		expiresAt                time.Time
		selectableUntil          *time.Time
		selectedQuoteID, orderID *uuid.UUID
		closedReason             *string
		createdAt, updatedAt     time.Time
	)

	err := q.pool.QueryRow(ctx, `
		SELECT version, prescription_id, pet_id, guardian_id, country, mode, status,
		       items, area, expires_at, selectable_until, selected_quote_id, order_id,
		       closed_reason, created_at, updated_at
		FROM read_pharmacyorders.quote_requests
		WHERE id = $1`, id).Scan(
		&version, &rxID, &petID, &guardianID, &country, &mode, &status,
		&itemsJSON, &areaJSON, &expiresAt, &selectableUntil, &selectedQuoteID, &orderID,
		&closedReason, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, kernel.NewDomainError("QUOTE_REQUEST_NOT_FOUND", "Solicitud de cotización no encontrada.")
	}
	if err != nil {
		return nil, fmt.Errorf("get quote request: %w", err)
	}

	var items []domain.RequestedItem
	_ = json.Unmarshal(itemsJSON, &items)
	var area domain.Area
	_ = json.Unmarshal(areaJSON, &area)

	// Cargar invitaciones
	invRows, err := q.pool.Query(ctx, `
		SELECT organization_id, status, channel, sent_at, attempts, last_error, responded_at
		FROM read_pharmacyorders.invitations
		WHERE quote_request_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("get invitations: %w", err)
	}
	defer invRows.Close()

	var invitations []domain.Invitation
	for invRows.Next() {
		var inv domain.Invitation
		var ch *string
		if err := invRows.Scan(&inv.OrgID, &inv.Status, &ch, &inv.SentAt, &inv.Attempts, &inv.LastError, &inv.RespondedAt); err != nil {
			return nil, err
		}
		if ch != nil {
			inv.Channel = *ch
		}
		invitations = append(invitations, inv)
	}

	// Cargar cotizaciones
	qRows, err := q.pool.Query(ctx, `
		SELECT id, organization_id, lines, subtotal, delivery_fee, total, currency,
		       eta, pickup, valid_until, notes, completeness, received_at, superseded
		FROM read_pharmacyorders.quotes
		WHERE quote_request_id = $1 AND superseded = false
		ORDER BY total ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("get quotes: %w", err)
	}
	defer qRows.Close()

	var quotes []domain.Quote
	for qRows.Next() {
		var (
			quo                   domain.Quote
			linesJSON, pickupJSON []byte
		)
		if err := qRows.Scan(
			&quo.ID, &quo.OrgID, &linesJSON, &quo.Subtotal, &quo.DeliveryFee, &quo.Total,
			&quo.Currency, &quo.ETA, &pickupJSON, &quo.ValidUntil, &quo.Notes,
			&quo.Completeness, &quo.ReceivedAt, &quo.Superseded,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(linesJSON, &quo.Lines)
		if len(pickupJSON) > 0 {
			_ = json.Unmarshal(pickupJSON, &quo.Pickup)
		}
		quotes = append(quotes, quo)
	}

	return &QuoteRequestDetailView{
		ID:              id,
		Version:         version,
		PrescriptionID:  rxID,
		PetID:           petID,
		GuardianID:      guardianID,
		Country:         country,
		Items:           items,
		Mode:            domain.FulfillmentMode(mode),
		Area:            area,
		Status:          domain.QuoteRequestStatus(status),
		ExpiresAt:       expiresAt,
		SelectableUntil: selectableUntil,
		SelectedQuoteID: selectedQuoteID,
		OrderID:         orderID,
		ClosedReason:    closedReason,
		Invitations:     invitations,
		Quotes:          quotes,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// ListGuardianQuoteRequests lista las solicitudes de un tutor con filtros y paginación.
func (q *Queries) ListGuardianQuoteRequests(ctx context.Context, guardianID uuid.UUID, status string, limit, offset int) ([]QuoteRequestDetailView, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	query := `
		SELECT id
		FROM read_pharmacyorders.quote_requests
		WHERE guardian_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := q.pool.Query(ctx, query, guardianID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list guardian quote requests: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	var results []QuoteRequestDetailView
	for _, id := range ids {
		detail, err := q.GetQuoteRequest(ctx, id)
		if err == nil && detail != nil {
			results = append(results, *detail)
		}
	}
	return results, nil
}

// GetOrder consulta el detalle de un pedido.
func (q *Queries) GetOrder(ctx context.Context, id uuid.UUID) (*OrderDetailView, error) {
	var (
		rxID, quoteID, qrID, petID, guardianID, orgID           uuid.UUID
		mode, status, phone                                                 string
		vInt                                                                int64
		linesJSON, totalsJSON, addrJSON, timelineJSON, dispJSON, ratingJSON []byte
		partnerRef                                                          *string
		createdAt, updatedAt                                                time.Time
		orgName, orgPhone, orgCity                                          string
	)

	err := q.pool.QueryRow(ctx, `
		SELECT o.version, o.quote_request_id, o.quote_id, o.prescription_id, o.pet_id,
		       o.guardian_id, o.pharmacy_org_id, o.mode, o.delivery_address, o.contact_phone,
		       o.lines, o.totals, o.status, o.timeline, o.dispensation, o.partner_ref,
		       o.rating, o.created_at, o.updated_at,
		       p.display_name, COALESCE(p.phone, ''), COALESCE(p.city, '')
		FROM read_pharmacyorders.orders o
		JOIN read_partnernetwork.organizations p ON p.id = o.pharmacy_org_id
		WHERE o.id = $1`, id).Scan(
		&vInt, &qrID, &quoteID, &rxID, &petID, &guardianID, &orgID,
		&mode, &addrJSON, &phone, &linesJSON, &totalsJSON, &status,
		&timelineJSON, &dispJSON, &partnerRef, &ratingJSON, &createdAt, &updatedAt,
		&orgName, &orgPhone, &orgCity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, kernel.NewDomainError("ORDER_NOT_FOUND", "Pedido no encontrado.")
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}

	var lines []domain.OrderLine
	_ = json.Unmarshal(linesJSON, &lines)
	var totals domain.OrderTotals
	_ = json.Unmarshal(totalsJSON, &totals)
	var addr *domain.Address
	if len(addrJSON) > 0 {
		_ = json.Unmarshal(addrJSON, &addr)
	}
	var timeline []domain.OrderTimelineEntry
	_ = json.Unmarshal(timelineJSON, &timeline)
	var disp *domain.DispensationRecord
	if len(dispJSON) > 0 {
		_ = json.Unmarshal(dispJSON, &disp)
	}
	var rating *OrderRatingView
	if len(ratingJSON) > 0 && string(ratingJSON) != "null" {
		_ = json.Unmarshal(ratingJSON, &rating)
	}

	return &OrderDetailView{
		ID:              id,
		Version:         vInt,
		QuoteRequestID:  qrID,
		QuoteID:         quoteID,
		PrescriptionID:  rxID,
		PetID:           petID,
		GuardianID:      guardianID,
		PharmacyOrgID:   orgID,
		PharmacyName:    orgName,
		PharmacyPhone:   orgPhone,
		PharmacyCity:    orgCity,
		Mode:            domain.FulfillmentMode(mode),
		DeliveryAddress: addr,
		ContactPhone:    phone,
		Lines:           lines,
		Totals:          totals,
		Status:          domain.OrderStatus(status),
		Timeline:        timeline,
		Dispensation:    disp,
		PartnerRef:      partnerRef,
		Rating:          rating,
		PaymentNotice:   "El pago se realiza en la farmacia",
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// ListGuardianOrders consulta los pedidos de un tutor.
func (q *Queries) ListGuardianOrders(ctx context.Context, guardianID uuid.UUID, status string, limit, offset int) ([]OrderDetailView, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := q.pool.Query(ctx, `
		SELECT id
		FROM read_pharmacyorders.orders
		WHERE guardian_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`, guardianID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list guardian orders: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	var results []OrderDetailView
	for _, id := range ids {
		o, err := q.GetOrder(ctx, id)
		if err == nil && o != nil {
			results = append(results, *o)
		}
	}
	return results, nil
}

// ListPharmacyInbox consulta las solicitudes recibidas por una farmacia según estado de invitación (FE-007).
func (q *Queries) ListPharmacyInbox(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]PharmacyInboxItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if status == "" {
		status = "pending"
	}

	rows, err := q.pool.Query(ctx, `
		SELECT qr.id, qr.created_at, qr.expires_at, i.status, qr.mode, qr.area, qr.items,
		       rx.id, rx.source, (rx.status = 'issued') as verified,
		       p.species, p.weight_kg, p.date_of_birth
		FROM read_pharmacyorders.invitations i
		JOIN read_pharmacyorders.quote_requests qr ON qr.id = i.quote_request_id
		JOIN read_prescriptions.prescriptions rx ON rx.id = qr.prescription_id
		JOIN read_petregistry.pets p ON p.id = qr.pet_id
		WHERE i.organization_id = $1 AND ($2 = '' OR i.status = $2)
		ORDER BY qr.created_at DESC
		LIMIT $3 OFFSET $4`, orgID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list pharmacy inbox: %w", err)
	}
	defer rows.Close()

	var items []PharmacyInboxItem
	for rows.Next() {
		var (
			qrID, rxID              uuid.UUID
			createdAt, expiresAt    time.Time
			invStatus, mode, source string
			verified                bool
			areaJSON, itemsJSON     []byte
			species                 string
			weight                  *float64
			dob                     *time.Time
		)
		if err := rows.Scan(
			&qrID, &createdAt, &expiresAt, &invStatus, &mode, &areaJSON, &itemsJSON,
			&rxID, &source, &verified, &species, &weight, &dob,
		); err != nil {
			return nil, err
		}

		var reqItems []domain.RequestedItem
		_ = json.Unmarshal(itemsJSON, &reqItems)
		var area domain.Area
		_ = json.Unmarshal(areaJSON, &area)

		var ageMonths *int
		if dob != nil {
			months := int(time.Since(*dob).Hours() / (24 * 30))
			ageMonths = &months
		}

		animal := domain.AnimalSnapshot{
			Species:   species,
			WeightKg:  weight,
			AgeMonths: ageMonths,
		}

		var verifyURL *string
		if source == "issued" {
			u := fmt.Sprintf("/verify/prescriptions/%s", rxID)
			verifyURL = &u
		}

		inboxLines := make([]InboxItemLine, 0, len(reqItems))
		for _, it := range reqItems {
			line := InboxItemLine{
				ItemID:              it.ItemID,
				CatalogID:           it.CatalogID,
				Name:                it.NameSnapshot,
				Form:                it.Form,
				Concentration:       it.Concentration,
				FreeText:            it.FreeText,
				Dose:                it.Dose,
				Frequency:           it.Frequency,
				Quantity:            it.Quantity,
				Unit:                it.Unit,
				SubstitutionAllowed: it.SubstitutionAllowed,
			}
			// Buscar mapeo SKU si tiene catalog_id
			if it.CatalogID != nil {
				var pm ProductMappingView
				err := q.pool.QueryRow(ctx, `
					SELECT catalog_id, partner_sku, partner_name, base_price, active
					FROM read_partnernetwork.product_mappings
					WHERE organization_id = $1 AND catalog_id = $2`, orgID, *it.CatalogID).Scan(
					&pm.CatalogID, &pm.PartnerSKU, &pm.PartnerName, &pm.BasePrice, &pm.Active,
				)
				if err == nil {
					line.MyMapping = &pm
				}
			}
			inboxLines = append(inboxLines, line)
		}

		items = append(items, PharmacyInboxItem{
			QuoteRequestID:   qrID,
			ReceivedAt:       createdAt,
			ExpiresAt:        expiresAt,
			InvitationStatus: domain.InvitationStatus(invStatus),
			Pet:              animal,
			Items:            inboxLines,
			Mode:             domain.FulfillmentMode(mode),
			Area:             area,
			Prescription: InboxPrescriptionRef{
				ID:        rxID,
				Source:    source,
				Verified:  verified,
				VerifyURL: verifyURL,
			},
		})
	}
	return items, nil
}

// ListPharmacyOrders lista pedidos asignados a una farmacia.
func (q *Queries) ListPharmacyOrders(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]OrderDetailView, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := q.pool.Query(ctx, `
		SELECT id
		FROM read_pharmacyorders.orders
		WHERE pharmacy_org_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`, orgID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list pharmacy orders: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	var results []OrderDetailView
	for _, id := range ids {
		o, err := q.GetOrder(ctx, id)
		if err == nil && o != nil {
			results = append(results, *o)
		}
	}
	return results, nil
}

// QuoteOptionsView modela las opciones para cotizar una receta médica.
type QuoteOptionsView struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	GuardianID     uuid.UUID          `json:"guardian_id"`
	Items          []QuoteOptionItem  `json:"items"`
	Pharmacies     []EligiblePharmacy `json:"pharmacies"`
}

type QuoteOptionItem struct {
	PrescriptionItemID uuid.UUID `json:"prescription_item_id"`
	MedicationName     string    `json:"medication_name"`
	Dose               string    `json:"dose"`
	Quantity           float64   `json:"quantity"`
	Unit               string    `json:"unit"`
	IsControlled       bool      `json:"is_controlled"`
}

type EligiblePharmacy struct {
	PharmacyID              uuid.UUID `json:"pharmacy_id"`
	Name                    string    `json:"name"`
	Address                 string    `json:"address"`
	Rating                  float64   `json:"rating"`
	Active                  bool      `json:"active"`
	FulfillmentTypes        []string  `json:"fulfillment_types"`
	AverageQuoteTimeSeconds int       `json:"average_quote_time_seconds"`
	OnTimeDeliveryRate      float64   `json:"on_time_delivery_rate"`
}

// GetPrescriptionQuoteOptions consulta los ítems cotizables y las farmacias elegibles para una receta (PRD-021).
func (q *Queries) GetPrescriptionQuoteOptions(ctx context.Context, prescriptionID uuid.UUID) (*QuoteOptionsView, error) {
	var (
		guardianID uuid.UUID
		itemsJSON  []byte
	)
	err := q.pool.QueryRow(ctx, `
		SELECT guardian_id, items
		FROM read_prescriptions.prescriptions
		WHERE id = $1`, prescriptionID).Scan(&guardianID, &itemsJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &kernel.NotFoundError{Resource: "Prescription " + prescriptionID.String()}
		}
		return nil, fmt.Errorf("get prescription for quote options: %w", err)
	}

	var rawItems []struct {
		ID             uuid.UUID `json:"id"`
		Name           string    `json:"name"`
		Dose           string    `json:"dose"`
		Quantity       float64   `json:"quantity"`
		Unit           string    `json:"unit"`
		IsControlled   bool      `json:"is_controlled"`
		ControlledFlag bool      `json:"controlled"`
	}
	_ = json.Unmarshal(itemsJSON, &rawItems)

	optItems := make([]QuoteOptionItem, 0, len(rawItems))
	for _, it := range rawItems {
		optItems = append(optItems, QuoteOptionItem{
			PrescriptionItemID: it.ID,
			MedicationName:     it.Name,
			Dose:               it.Dose,
			Quantity:           it.Quantity,
			Unit:               it.Unit,
			IsControlled:       it.IsControlled || it.ControlledFlag,
		})
	}

	rows, err := q.pool.Query(ctx, `
		SELECT o.id, o.display_name, COALESCE(o.address, ''),
		       COALESCE(s.rating_avg, 5.0),
		       COALESCE(s.avg_response_minutes, 15.0),
		       COALESCE(s.quote_rate, 0.95)
		FROM read_partnernetwork.organizations o
		LEFT JOIN read_pharmacyorders.partner_sla s ON s.organization_id = o.id
		WHERE o.type = 'pharmacy' AND o.status = 'active'
		LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("list eligible pharmacies: %w", err)
	}
	defer rows.Close()

	var pharmacies []EligiblePharmacy
	for rows.Next() {
		var (
			id                   uuid.UUID
			name, addr           string
			rating, avgResp, qRt float64
		)
		if err := rows.Scan(&id, &name, &addr, &rating, &avgResp, &qRt); err != nil {
			return nil, err
		}
		pharmacies = append(pharmacies, EligiblePharmacy{
			PharmacyID:              id,
			Name:                    name,
			Address:                 addr,
			Rating:                  rating,
			Active:                  true,
			FulfillmentTypes:        []string{"delivery", "pickup"},
			AverageQuoteTimeSeconds: int(avgResp * 60),
			OnTimeDeliveryRate:      qRt,
		})
	}

	return &QuoteOptionsView{
		PrescriptionID: prescriptionID,
		GuardianID:     guardianID,
		Items:          optItems,
		Pharmacies:     pharmacies,
	}, nil
}

// PharmacyIntegrationSettingsView representa la configuración técnica de integración de una farmacia (ARQ-003).
type PharmacyIntegrationSettingsView struct {
	Level                 string             `json:"level"`
	WebhookURL            *string            `json:"webhook_url,omitempty"`
	CatalogMappingEnabled bool               `json:"catalog_mapping_enabled"`
	DeliveryAreas         []DeliveryAreaView `json:"delivery_areas"`
}

type DeliveryAreaView struct {
	City        string   `json:"city"`
	PostalCodes []string `json:"postal_codes"`
}

// GetIntegrationSettings consulta la configuración de integración.
func (q *Queries) GetIntegrationSettings(ctx context.Context, orgID uuid.UUID) (*PharmacyIntegrationSettingsView, error) {
	var (
		level      string
		webhookURL *string
		catEnabled bool
		areasJSON  []byte
	)
	err := q.pool.QueryRow(ctx, `
		SELECT level, webhook_url, catalog_mapping_enabled, delivery_areas
		FROM read_pharmacyorders.pharmacy_integrations
		WHERE organization_id = $1`, orgID).Scan(&level, &webhookURL, &catEnabled, &areasJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &PharmacyIntegrationSettingsView{
				Level:                 "portal",
				CatalogMappingEnabled: true,
				DeliveryAreas:         []DeliveryAreaView{},
			}, nil
		}
		return nil, fmt.Errorf("get integration settings: %w", err)
	}

	var areas []DeliveryAreaView
	if len(areasJSON) > 0 {
		_ = json.Unmarshal(areasJSON, &areas)
	}
	return &PharmacyIntegrationSettingsView{
		Level:                 level,
		WebhookURL:            webhookURL,
		CatalogMappingEnabled: catEnabled,
		DeliveryAreas:         areas,
	}, nil
}

// UpdateIntegrationSettings guarda la configuración de integración de una farmacia.
func (q *Queries) UpdateIntegrationSettings(ctx context.Context, orgID uuid.UUID, level string, webhookURL *string, catalogMapping bool, deliveryAreas []DeliveryAreaView) error {
	areasJSON, err := json.Marshal(deliveryAreas)
	if err != nil {
		return err
	}
	_, err = q.pool.Exec(ctx, `
		INSERT INTO read_pharmacyorders.pharmacy_integrations (organization_id, level, webhook_url, catalog_mapping_enabled, delivery_areas, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (organization_id) DO UPDATE
		SET level = EXCLUDED.level,
		    webhook_url = EXCLUDED.webhook_url,
		    catalog_mapping_enabled = EXCLUDED.catalog_mapping_enabled,
		    delivery_areas = EXCLUDED.delivery_areas,
		    updated_at = now()`,
		orgID, level, webhookURL, catalogMapping, areasJSON)
	return err
}

// ProductMappingItemView describe un producto mapeado por la farmacia (DOM-011).
type ProductMappingItemView struct {
	CatalogID    uuid.UUID `json:"catalog_id"`
	StandardName string    `json:"standard_name"`
	PharmacySKU  string    `json:"pharmacy_sku"`
	PharmacyName string    `json:"pharmacy_name"`
	PriceCents   int       `json:"price_cents"`
	InStock      bool      `json:"in_stock"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListProductMappings lista los mapeos de catálogo configurados por la farmacia.
func (q *Queries) ListProductMappings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]ProductMappingItemView, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int
	err := q.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM read_partnernetwork.product_mappings
		WHERE organization_id = $1`, orgID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := q.pool.Query(ctx, `
		SELECT pm.catalog_id, m.name, pm.partner_sku, COALESCE(pm.partner_name, ''),
		       (COALESCE(pm.base_price, 0) * 100)::int, pm.active, pm.updated_at
		FROM read_partnernetwork.product_mappings pm
		JOIN ref.medications m ON m.id = pm.catalog_id
		WHERE pm.organization_id = $1
		ORDER BY pm.updated_at DESC
		LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []ProductMappingItemView
	for rows.Next() {
		var it ProductMappingItemView
		if err := rows.Scan(
			&it.CatalogID, &it.StandardName, &it.PharmacySKU, &it.PharmacyName,
			&it.PriceCents, &it.InStock, &it.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, nil
}

// UpsertProductMapping crea o actualiza un mapeo de catálogo para una farmacia.
func (q *Queries) UpsertProductMapping(ctx context.Context, orgID, catalogID uuid.UUID, sku, name string, priceCents int, inStock bool) error {
	basePrice := float64(priceCents) / 100.0
	_, err := q.pool.Exec(ctx, `
		INSERT INTO read_partnernetwork.product_mappings (organization_id, catalog_id, partner_sku, partner_name, base_price, active, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (organization_id, catalog_id) DO UPDATE
		SET partner_sku = EXCLUDED.partner_sku,
		    partner_name = EXCLUDED.partner_name,
		    base_price = EXCLUDED.base_price,
		    active = EXCLUDED.active,
		    updated_at = now()`,
		orgID, catalogID, sku, name, basePathPrice(basePrice), inStock)
	return err
}

func basePathPrice(f float64) float64 { return f }

// DeleteProductMapping elimina un mapeo de catálogo.
func (q *Queries) DeleteProductMapping(ctx context.Context, orgID, catalogID uuid.UUID) error {
	_, err := q.pool.Exec(ctx, `
		DELETE FROM read_partnernetwork.product_mappings
		WHERE organization_id = $1 AND catalog_id = $2`, orgID, catalogID)
	return err
}
