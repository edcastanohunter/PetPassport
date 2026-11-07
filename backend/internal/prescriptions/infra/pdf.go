package infra

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"rsc.io/qr"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
)

//go:embed templates/prescription.typ
var prescriptionTemplate []byte

// PrescriptionDocument contiene los datos renderizados en el Typst (PP-F4-ARQ-011).
type PrescriptionDocument = app.PrescriptionDocument

// PrescriptionDocItem describe un ítem de medicación en el PDF.
type PrescriptionDocItem = app.PrescriptionDocItem

// PrescriptionPDFKey genera la clave en object storage para el PDF de la receta.
func PrescriptionPDFKey(prescriptionID uuid.UUID, version int64) string {
	return fmt.Sprintf("prescriptions/%s/v%d.pdf", prescriptionID, version)
}

// PrescriptionPDFGenerator orquesta el renderizado con Typst y subida a S3/MinIO.
type PrescriptionPDFGenerator struct {
	renderer   *pdfrender.Typst
	storage    storage.ObjectStorage
	verifyBase string
}

var _ app.PDFGenerator = (*PrescriptionPDFGenerator)(nil)

// NewPrescriptionPDFGenerator construye el generador de PDF.
func NewPrescriptionPDFGenerator(renderer *pdfrender.Typst, storage storage.ObjectStorage, verifyBase string) *PrescriptionPDFGenerator {
	return &PrescriptionPDFGenerator{
		renderer:   renderer,
		storage:    storage,
		verifyBase: strings.TrimRight(verifyBase, "/"),
	}
}

// Render genera los bytes del PDF a partir del modelo del documento.
func (g *PrescriptionPDFGenerator) Render(ctx context.Context, doc PrescriptionDocument) ([]byte, error) {
	if doc.VerifyURL == "" && g.verifyBase != "" {
		doc.VerifyURL = fmt.Sprintf("%s/verify/prescriptions/%s?h=%s", g.verifyBase, doc.ID, doc.ContentHash)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal prescription doc: %w", err)
	}

	qrCode, err := qr.Encode(doc.VerifyURL, qr.M)
	if err != nil {
		return nil, fmt.Errorf("encode qr: %w", err)
	}

	pdfBytes, err := g.renderer.Render(ctx, prescriptionTemplate, raw, pdfrender.File{
		Name: "qr.png",
		Data: qrCode.PNG(),
	})
	if err != nil {
		return nil, fmt.Errorf("render typst: %w", err)
	}

	return pdfBytes, nil
}

// GenerateAndStore renderiza el PDF y lo persiste en object storage, devolviendo la clave.
func (g *PrescriptionPDFGenerator) GenerateAndStore(ctx context.Context, doc PrescriptionDocument) (string, error) {
	pdfBytes, err := g.Render(ctx, doc)
	if err != nil {
		return "", err
	}

	rxID, err := uuid.Parse(doc.ID)
	if err != nil {
		return "", fmt.Errorf("invalid prescription id: %w", err)
	}

	key := PrescriptionPDFKey(rxID, doc.Version)
	if g.storage != nil {
		if err := g.storage.Put(ctx, key, "application/pdf", bytes.NewReader(pdfBytes), int64(len(pdfBytes))); err != nil {
			return "", fmt.Errorf("put pdf to storage: %w", err)
		}
	}

	return key, nil
}
