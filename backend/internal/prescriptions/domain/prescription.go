package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// StreamType es el tipo de stream en el Event Store (rx-<PrescriptionID>, DOM-004, ADR-018).
const StreamType = "Prescription"

// Prescription es la raíz de agregado del documento de prescripción médica (PP-F4-DOM-004, ADR-018).
// Stream rx-<PrescriptionID>.
type Prescription struct {
	ID            uuid.UUID
	Version       int64
	PetID         uuid.UUID
	GuardianID    uuid.UUID
	Country       string
	Source        Source
	Status        Status
	IssuerID      *uuid.UUID
	IssuerOrgID   *uuid.UUID
	CredentialRef *CredentialRef
	EncounterID   *uuid.UUID
	IssuedAt      *time.Time
	Signature     *Signature
	Images        []ImageRef
	Extraction    *Extraction
	Confirmation  *Confirmation
	Validation    *PharmacistValidation
	ExternalMeta  *ExternalMeta
	Items         []PrescriptionItem
	Validity      Validity
	RefillsUsed   int
	Dispensations []Dispensation
	Notes         *string

	// Estado derivado para control de saldos por refill
	ItemsRemaining map[string]float64
	uncommitted    []any
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (p *Prescription) UncommittedEvents() []any { return p.uncommitted }

// MarkCommitted limpia los eventos no confirmados tras el Append.
func (p *Prescription) MarkCommitted() { p.uncommitted = nil }

func (p *Prescription) record(evt any) {
	p.Apply(evt)
	p.uncommitted = append(p.uncommitted, evt)
}

// NewPrescription construye una instancia vacía del agregado (versión 0).
func NewPrescription() *Prescription {
	return &Prescription{
		ItemsRemaining: make(map[string]float64),
	}
}

// DraftIssuedPrescription inicia un borrador de receta emitida por un veterinario (PP-F4-DOM-004).
func (p *Prescription) DraftIssuedPrescription(
	id uuid.UUID,
	petID uuid.UUID,
	guardianID uuid.UUID,
	issuerID uuid.UUID,
	issuerOrgID uuid.UUID,
	encounterID *uuid.UUID,
	country string,
	items []PrescriptionItem,
	validity *Validity,
	notes *string,
	rules *RegulatoryRule,
	hasOrgAccess bool,
	credRef *CredentialRef,
	hasSignedPlan bool,
	now time.Time,
) (*PrescriptionDrafted, error) {
	if p.Version > 0 {
		return nil, ErrPrescriptionImmutable
	}

	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		return nil, kernel.NewDomainError("INVALID_COUNTRY", "El código de país debe tener 2 letras.")
	}

	// Invariante R1: Credencial profesional verificada
	if credRef == nil {
		return nil, ErrCredentialRequired
	}

	// Invariante R2: La organización debe tener acceso activo a la mascota
	if !hasOrgAccess {
		return nil, ErrOrgAccessDenied
	}

	// Invariante R3: Si se deriva de un encuentro clínico, debe estar firmado
	if encounterID != nil && !hasSignedPlan {
		return nil, ErrEncounterNotSigned
	}

	// Invariante R5: Validación estricta de medicamentos
	if err := validateItems(items); err != nil {
		return nil, err
	}

	// Invariante R4: Validación de vigencia y refills
	effectiveValidity, err := resolveValidity(validity, rules, now)
	if err != nil {
		return nil, err
	}

	var trimmedNotes *string
	if notes != nil {
		tn := strings.TrimSpace(*notes)
		if len(tn) > 500 {
			return nil, kernel.NewDomainError("NOTES_TOO_LONG", "Las notas no pueden superar 500 caracteres.")
		}
		if tn != "" {
			trimmedNotes = &tn
		}
	}

	evt := &PrescriptionDrafted{
		PrescriptionID: id,
		PetID:          petID,
		GuardianID:     guardianID,
		Source:         SourceIssued,
		IssuerID:       &issuerID,
		IssuerOrgID:    &issuerOrgID,
		EncounterID:    encounterID,
		Country:        country,
		Items:          items,
		Validity:       effectiveValidity,
		Notes:          trimmedNotes,
	}

	p.record(evt)
	return evt, nil
}

// UpdateDraft actualiza los datos de un borrador de receta antes de su emisión.
func (p *Prescription) UpdateDraft(
	items []PrescriptionItem,
	validity *Validity,
	notes *string,
	rules *RegulatoryRule,
	now time.Time,
) (*PrescriptionDraftUpdated, error) {
	if p.Status != StatusDraft {
		return nil, ErrPrescriptionNotDraft
	}

	if items != nil {
		if err := validateItems(items); err != nil {
			return nil, err
		}
	} else {
		items = p.Items
	}

	effectiveValidity := p.Validity
	if validity != nil {
		v, err := resolveValidity(validity, rules, now)
		if err != nil {
			return nil, err
		}
		effectiveValidity = v
	}

	var trimmedNotes *string
	if notes != nil {
		tn := strings.TrimSpace(*notes)
		if len(tn) > 500 {
			return nil, kernel.NewDomainError("NOTES_TOO_LONG", "Las notas no pueden superar 500 caracteres.")
		}
		if tn != "" {
			trimmedNotes = &tn
		}
	} else {
		trimmedNotes = p.Notes
	}

	evt := &PrescriptionDraftUpdated{
		PrescriptionID: p.ID,
		Items:          items,
		Validity:       effectiveValidity,
		Notes:          trimmedNotes,
	}

	p.record(evt)
	return evt, nil
}

// IssuePrescription formaliza la emisión de la receta con la firma del veterinario (PP-F4-DOM-004).
func (p *Prescription) IssuePrescription(
	signerID uuid.UUID,
	credRef *CredentialRef,
	rules *RegulatoryRule,
	contentHash string,
	now time.Time,
) (*PrescriptionIssued, error) {
	if p.Status != StatusDraft {
		return nil, ErrPrescriptionNotDraft
	}
	if p.IssuerID == nil || signerID != *p.IssuerID {
		return nil, kernel.NewDomainError("FORBIDDEN", "Solo el veterinario emisor puede firmar la receta.")
	}

	// Invariante R1: Verificación de credencial al firmar
	if credRef == nil {
		return nil, ErrCredentialRequired
	}

	// Invariante R4: Vigencia dentro de límites regulatorios
	if err := checkValidityBounds(p.Validity, rules, now); err != nil {
		return nil, err
	}

	if len(p.Items) == 0 {
		return nil, ErrPrescriptionIncomplete
	}

	if strings.TrimSpace(contentHash) == "" {
		return nil, kernel.NewDomainError("INVALID_CONTENT_HASH", "El content hash es obligatorio para firmar.")
	}

	evt := &PrescriptionIssued{
		PrescriptionID: p.ID,
		IssuerID:       signerID,
		Credential:     *credRef,
		IssuedAt:       now,
		ValidUntil:     p.Validity.ValidUntil,
		RefillsAllowed: p.Validity.RefillsAllowed,
		Items:          p.Items,
		StreamVersion:  p.Version + 1,
		ContentHash:    contentHash,
	}

	p.record(evt)
	return evt, nil
}

// RevokePrescription revoca una receta emitida (PP-F4-DOM-004).
func (p *Prescription) RevokePrescription(reason string, by uuid.UUID, isSuperAdmin bool) (*PrescriptionRevoked, error) {
	if p.Status != StatusIssued && p.Status != StatusConfirmed {
		return nil, ErrPrescriptionImmutable
	}

	// Invariante R6: Revocación solo sin dispensaciones salvo super_admin
	if len(p.Dispensations) > 0 && !isSuperAdmin {
		return nil, ErrPrescriptionDispensed
	}

	reason = strings.TrimSpace(reason)
	if len(reason) < 3 {
		return nil, kernel.NewDomainError("REASON_REQUIRED", "El motivo de revocación debe tener al menos 3 caracteres.")
	}

	evt := &PrescriptionRevoked{
		PrescriptionID: p.ID,
		Reason:         reason,
		By:             by,
	}

	p.record(evt)
	return evt, nil
}

// UploadExternalPrescription registra la subida inicial de una receta externa por el tutor (PP-F4-DOM-004).
func (p *Prescription) UploadExternalPrescription(
	id uuid.UUID,
	petID uuid.UUID,
	guardianID uuid.UUID,
	images []ImageRef,
	country string,
) (*ExternalPrescriptionUploaded, error) {
	if p.Version > 0 {
		return nil, ErrPrescriptionImmutable
	}
	if len(images) == 0 || len(images) > 5 {
		return nil, ErrExternalUploadLimit
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		return nil, kernel.NewDomainError("INVALID_COUNTRY", "El país debe ser un código de 2 letras.")
	}

	evt := &ExternalPrescriptionUploaded{
		PrescriptionID: id,
		PetID:          petID,
		GuardianID:     guardianID,
		Images:         images,
		Country:        country,
	}

	p.record(evt)
	return evt, nil
}

// ConfirmExternalPrescription confirma y ajusta los ítems de una receta externa (PP-F4-DOM-006).
func (p *Prescription) ConfirmExternalPrescription(
	items []PrescriptionItem,
	validity Validity,
	externalMeta ExternalMeta,
	diff map[string]any,
	rules *RegulatoryRule,
	now time.Time,
) (*ExternalPrescriptionConfirmed, error) {
	if p.Status != StatusDraft && p.Status != StatusConfirmed {
		return nil, ErrPrescriptionNotDraft
	}
	if p.Source != SourceExternal {
		return nil, kernel.NewDomainError("INVALID_SOURCE", "Solo recetas externas pueden ser confirmadas por el tutor.")
	}
	if err := validateItems(items); err != nil {
		return nil, err
	}
	if err := checkValidityBounds(validity, rules, now); err != nil {
		return nil, err
	}
	evt := &ExternalPrescriptionConfirmed{
		PrescriptionID: p.ID,
		Items:          items,
		Validity:       validity,
		ExternalMeta:   externalMeta,
		Diff:           diff,
	}
	p.record(evt)
	return evt, nil
}

// RecordDispensation registra la entrega efectuada por una farmacia (PP-F4-DOM-004).
func (p *Prescription) RecordDispensation(
	orderID uuid.UUID,
	pharmacyOrgID uuid.UUID,
	lines []DispensationLine,
	now time.Time,
) ([]any, error) {
	if p.Status != StatusIssued && p.Status != StatusConfirmed {
		return nil, ErrPrescriptionNotQuotable
	}

	// Comprobar vigencia
	today := kernel.CivilDateOf(now)
	if p.Validity.ValidUntil.Before(today) {
		return nil, ErrPrescriptionExpired
	}

	// Invariante R8: Refills no superados
	if p.RefillsUsed > p.Validity.RefillsAllowed {
		return nil, ErrRefillsExceeded
	}

	if len(lines) == 0 {
		return nil, kernel.NewDomainError("DISPENSATION_EMPTY", "La dispensación debe contener al menos una línea.")
	}

	// Invariante R7: Cantidad no supera el saldo pendiente del refill actual
	itemTotals := make(map[string]float64)
	for _, it := range p.Items {
		itemTotals[it.ItemID] = it.QuantityToDispense.Value
	}

	partial := false
	for _, l := range lines {
		total, exists := itemTotals[l.ItemID]
		if !exists {
			return nil, kernel.NewDomainError("UNKNOWN_ITEM", "Ítem no pertenece a la receta.")
		}
		rem := p.ItemsRemaining[l.ItemID]
		if l.Qty <= 0 {
			return nil, kernel.NewDomainError("INVALID_QTY", "La cantidad dispensada debe ser mayor que cero.")
		}
		if l.Qty > rem {
			return nil, ErrDispensationExceeds
		}
		if l.Qty < total {
			partial = true
		}
	}

	newRefillsUsed := p.RefillsUsed

	// Comprobar si tras descontar estas líneas el refill actual se completa
	allDepleted := true
	for _, it := range p.Items {
		var dispensedThisLine float64
		for _, l := range lines {
			if l.ItemID == it.ItemID {
				dispensedThisLine += l.Qty
			}
		}
		remAfter := p.ItemsRemaining[it.ItemID] - dispensedThisLine
		if remAfter > 0.0001 {
			allDepleted = false
			break
		}
	}

	var events []any

	if allDepleted {
		newRefillsUsed++
	}

	dispEvt := &PrescriptionDispensed{
		PrescriptionID: p.ID,
		OrderID:        orderID,
		PharmacyOrgID:  pharmacyOrgID,
		Lines:          lines,
		Partial:        partial,
		RefillsUsed:    newRefillsUsed,
	}
	events = append(events, dispEvt)
	p.record(dispEvt)

	// Si se agotaron todos los refills y cantidades: emitir PrescriptionExhausted
	if allDepleted && newRefillsUsed > p.Validity.RefillsAllowed {
		exhaustEvt := &PrescriptionExhausted{PrescriptionID: p.ID}
		events = append(events, exhaustEvt)
		p.record(exhaustEvt)
	}

	return events, nil
}

// ExpirePrescription marca la receta como vencida si ha superado su fecha límite.
func (p *Prescription) ExpirePrescription(now time.Time) (*PrescriptionExpired, error) {
	if p.Status != StatusIssued && p.Status != StatusConfirmed {
		return nil, ErrPrescriptionImmutable
	}
	today := kernel.CivilDateOf(now)
	if !today.After(p.Validity.ValidUntil) {
		return nil, kernel.NewDomainError("NOT_EXPIRED", "La receta aún no ha alcanzado su fecha de expiración.")
	}

	evt := &PrescriptionExpired{PrescriptionID: p.ID}
	p.record(evt)
	return evt, nil
}

// Apply reconstruye el estado del agregado a partir de los eventos.
func (p *Prescription) Apply(event any) {
	p.Version++
	switch e := event.(type) {
	case *PrescriptionDrafted:
		p.ID = e.PrescriptionID
		p.PetID = e.PetID
		p.GuardianID = e.GuardianID
		p.Source = e.Source
		p.Status = StatusDraft
		p.IssuerID = e.IssuerID
		p.IssuerOrgID = e.IssuerOrgID
		p.EncounterID = e.EncounterID
		p.Country = e.Country
		p.Items = e.Items
		p.Validity = e.Validity
		p.Notes = e.Notes
		p.initRemaining()

	case *PrescriptionDraftUpdated:
		p.Items = e.Items
		p.Validity = e.Validity
		p.Notes = e.Notes
		p.initRemaining()

	case *PrescriptionIssued:
		p.Status = StatusIssued
		p.IssuerID = &e.IssuerID
		p.CredentialRef = &e.Credential
		p.IssuedAt = &e.IssuedAt
		p.Validity.ValidUntil = e.ValidUntil
		p.Validity.RefillsAllowed = e.RefillsAllowed
		p.Items = e.Items
		p.Signature = &Signature{
			SignedBy:      e.IssuerID,
			SignedAt:      e.IssuedAt,
			StreamVersion: e.StreamVersion,
			ContentHash:   e.ContentHash,
		}
		p.initRemaining()

	case *PrescriptionRevoked:
		p.Status = StatusRevoked

	case *ExternalPrescriptionUploaded:
		p.ID = e.PrescriptionID
		p.PetID = e.PetID
		p.GuardianID = e.GuardianID
		p.Source = SourceExternal
		p.Status = StatusDraft
		p.Images = e.Images
		p.Country = e.Country

	case *PrescriptionExtractionRecorded:
		p.Extraction = &e.Extraction

	case *PrescriptionReuploadRequested:
		// Se mantiene en borrador esperando nueva foto

	case *ExternalPrescriptionConfirmed:
		p.Status = StatusConfirmed
		p.Items = e.Items
		p.Validity = e.Validity
		p.ExternalMeta = &e.ExternalMeta
		p.Confirmation = &Confirmation{
			ConfirmedAt: time.Now(),
			Diff:        e.Diff,
		}
		p.initRemaining()

	case *ExternalPrescriptionReopened:
		p.Status = StatusDraft

	case *PrescriptionPharmacistValidated:
		p.Validation = &PharmacistValidation{
			By:     e.By,
			OrgID:  e.OrgID,
			Status: e.Status,
			Note:   e.Note,
			At:     time.Now(),
		}

	case *PrescriptionDispensed:
		p.Dispensations = append(p.Dispensations, Dispensation{
			OrderID:       e.OrderID,
			PharmacyOrgID: e.PharmacyOrgID,
			At:            time.Now(),
			Lines:         e.Lines,
			Partial:       e.Partial,
		})
		for _, l := range e.Lines {
			p.ItemsRemaining[l.ItemID] -= l.Qty
			if p.ItemsRemaining[l.ItemID] < 0 {
				p.ItemsRemaining[l.ItemID] = 0
			}
		}
		if e.RefillsUsed > p.RefillsUsed {
			p.RefillsUsed = e.RefillsUsed
			// Nuevo refill: reiniciar saldos a la cantidad original
			if p.RefillsUsed <= p.Validity.RefillsAllowed {
				p.initRemaining()
			}
		}

	case *PrescriptionExhausted:
		p.Status = StatusExhausted

	case *PrescriptionExpired:
		p.Status = StatusExpired
	}
}

func (p *Prescription) initRemaining() {
	if p.ItemsRemaining == nil {
		p.ItemsRemaining = make(map[string]float64)
	}
	for _, it := range p.Items {
		p.ItemsRemaining[it.ItemID] = it.QuantityToDispense.Value
	}
}

// Invariante R5: Validación de los ítems de prescripción
func validateItems(items []PrescriptionItem) error {
	if len(items) == 0 || len(items) > 20 {
		return ErrPrescriptionIncomplete
	}
	catalogSeen := make(map[uuid.UUID]bool)
	for i, it := range items {
		if it.ItemID == "" {
			return ErrItemInvalid
		}
		if it.CatalogID == nil && it.FreeText == nil {
			return ErrItemInvalid
		}
		if it.FreeText != nil {
			ft := strings.TrimSpace(*it.FreeText)
			if len(ft) < 3 {
				return ErrItemInvalid
			}
		}
		if it.CatalogID != nil {
			if catalogSeen[*it.CatalogID] {
				return kernel.NewDomainError("DUPLICATE_CATALOG_ITEM", "No se permite incluir el mismo medicamento del catálogo más de una vez en la receta.")
			}
			catalogSeen[*it.CatalogID] = true
		}
		if strings.TrimSpace(it.NameSnapshot) == "" {
			return ErrItemInvalid
		}
		if it.QuantityToDispense.Value <= 0 || strings.TrimSpace(it.QuantityToDispense.Unit) == "" {
			return ErrItemInvalid
		}
		if it.Dose.Value <= 0 || strings.TrimSpace(it.Dose.Unit) == "" {
			return ErrItemInvalid
		}
		if strings.TrimSpace(it.Route) == "" || strings.TrimSpace(it.Frequency) == "" {
			return ErrItemInvalid
		}
		if len(it.Instructions) > 300 {
			return kernel.NewDomainError("INSTRUCTIONS_TOO_LONG", "Las instrucciones no pueden superar 300 caracteres.")
		}
		items[i].NameSnapshot = strings.TrimSpace(it.NameSnapshot)
	}
	return nil
}

// Invariante R4: Resolución y comprobación de vigencia
func resolveValidity(val *Validity, rules *RegulatoryRule, now time.Time) (Validity, error) {
	defaultDays := 30
	maxDays := 180
	maxRefills := 5

	if rules != nil {
		if rules.DefaultValidityDays > 0 {
			defaultDays = rules.DefaultValidityDays
		}
		if rules.MaxValidityDays > 0 {
			maxDays = rules.MaxValidityDays
		}
		if rules.MaxRefills >= 0 {
			maxRefills = rules.MaxRefills
		}
	}

	today := kernel.CivilDateOf(now)
	if val == nil {
		return Validity{
			ValidUntil:     today.AddDays(defaultDays),
			RefillsAllowed: 0,
		}, nil
	}

	if val.ValidUntil == "" {
		val.ValidUntil = today.AddDays(defaultDays)
	}

	if val.RefillsAllowed < 0 || val.RefillsAllowed > maxRefills {
		return Validity{}, ErrValidityOutOfRules
	}

	maxLimit := today.AddDays(maxDays)
	if val.ValidUntil.Before(today) || val.ValidUntil.After(maxLimit) {
		return Validity{}, ErrValidityOutOfRules
	}

	return *val, nil
}

func checkValidityBounds(val Validity, rules *RegulatoryRule, now time.Time) error {
	today := kernel.CivilDateOf(now)
	maxDays := 180
	maxRefills := 5

	if rules != nil {
		if rules.MaxValidityDays > 0 {
			maxDays = rules.MaxValidityDays
		}
		if rules.MaxRefills >= 0 {
			maxRefills = rules.MaxRefills
		}
	}

	if val.RefillsAllowed < 0 || val.RefillsAllowed > maxRefills {
		return ErrValidityOutOfRules
	}

	maxLimit := today.AddDays(maxDays)
	if val.ValidUntil.Before(today) || val.ValidUntil.After(maxLimit) {
		return ErrValidityOutOfRules
	}
	return nil
}
