// Package invoicing define el puerto InvoicingProvider (PP-F5-DOM-011, ARQ-003,
// ADR-022 §4). Los adaptadores (dian_<proveedor>, pdflocal, fake) reciben DTOs y
// no conocen el dominio de Billing; devuelven los bytes del PDF/XML y es Billing
// quien los archiva con object lock.
package invoicing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Estados que un proveedor fiscal puede devolver para un documento.
const (
	StatusIssued   = "issued"   // emitido, pendiente de validación de la autoridad
	StatusAccepted = "accepted" // validado (CO: DIAN, con CUFE)
	StatusRejected = "rejected" // rechazado; RejectionReason explica el motivo
)

// ErrUnavailable indica que el proveedor fiscal no responde (caída o timeout):
// el llamante reintenta y, pasadas 24 h, emite en contingencia (ARQ-003).
var ErrUnavailable = errors.New("invoicing: proveedor fiscal no disponible")

// Party son los datos fiscales del cliente tal como figuran en el documento.
type Party struct {
	Kind      string `json:"kind"` // company | person
	LegalName string `json:"legal_name"`
	TaxIDKind string `json:"tax_id_kind"` // NIT | CC | …
	TaxID     string `json:"tax_id"`
	Country   string `json:"country"`
	Address   string `json:"address"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
	TaxRegime string `json:"tax_regime,omitempty"`
}

// Line es un concepto facturado. Amount puede ser negativo (descuentos).
type Line struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	Amount      float64 `json:"amount"`
}

// Totals desglosa el documento.
type Totals struct {
	Subtotal float64 `json:"subtotal"`
	Tax      float64 `json:"tax"`
	Total    float64 `json:"total"`
	Currency string  `json:"currency"`
	TaxRule  string  `json:"tax_rule"`
	TaxRate  float64 `json:"tax_rate"`
}

// Document es una factura a emitir. InvoiceID es la clave de idempotencia: emitir
// dos veces el mismo InvoiceID debe devolver el mismo documento.
type Document struct {
	InvoiceID   uuid.UUID
	ChargeID    uuid.UUID
	Customer    Party
	Lines       []Line
	Totals      Totals
	IssuedAt    time.Time
	PeriodStart *time.Time
	PeriodEnd   *time.Time
	// Contingency marca la factura como "pendiente de validación" (proveedor
	// fiscal caído > 24 h); solo la emite el adaptador local.
	Contingency bool
}

// CreditNoteDocument es una nota crédito sobre una factura emitida.
type CreditNoteDocument struct {
	CreditNoteID  uuid.UUID
	InvoiceNumber string
	Customer      Party
	Amount        float64
	Currency      string
	Reason        string
	IssuedAt      time.Time
}

// Issued es el resultado de emitir un documento.
type Issued struct {
	Number          string
	Status          string
	CUFE            string
	RejectionReason string
	PDF             []byte
	XML             []byte
}

// State es el estado consultado de un documento ya emitido.
type State struct {
	Status          string
	CUFE            string
	RejectionReason string
}

// Provider es el puerto de facturación fiscal.
type Provider interface {
	Name() string
	Issue(ctx context.Context, doc Document) (Issued, error)
	CreditNote(ctx context.Context, doc CreditNoteDocument) (Issued, error)
	Fetch(ctx context.Context, number string) (State, error)
}
