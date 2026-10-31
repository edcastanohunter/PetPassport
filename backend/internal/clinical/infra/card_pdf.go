package infra

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"rsc.io/qr"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

//go:embed templates/card.typ
var cardTemplate []byte

// CardDocument son los datos de la plantilla del carnet (data.json).
type CardDocument struct {
	PetID      string         `json:"pet_id"`
	PetName    string         `json:"pet_name"`
	Species    string         `json:"species"`
	Version    int            `json:"document_version"`
	Entries    []cardDocEntry `json:"entries"`
	NextDue    []cardDocDue   `json:"next_due"`
	Hash       string         `json:"content_hash"`
	VerifyURL  string         `json:"verify_url"`
	Generated  string         `json:"generated_at"`
	HasEntries bool           `json:"has_entries"`
}

type cardDocEntry struct {
	Date         string `json:"date"`
	Kind         string `json:"kind"`
	Product      string `json:"product"`
	Lot          string `json:"lot"`
	Dose         string `json:"dose"`
	Verification string `json:"verification"`
	Source       string `json:"source"`
}

type cardDocDue struct {
	Product string `json:"product"`
	Due     string `json:"due"`
	Status  string `json:"status"`
}

var (
	cardKindLabels         = map[string]string{"vaccine": "Vacuna", "deworming": "Desparasitación", "ectoparasite": "Antiparasitario externo"}
	cardVerificationLabels = map[string]string{"verified": "Verificado", "unverified": "No verificado", "disputed": "En disputa"}
	cardSourceLabels       = map[string]string{"recorded": "Clínica", "declared": "Tutor", "imported": "Importado"}
	cardDueLabels          = map[string]string{"upcoming": "próxima", "overdue": "vencida"}
)

// BuildCardDocument es puro (prueba de la plantilla): solo entradas activas,
// ordenadas como las devuelve la vista, con la huella del QR.
func BuildCardDocument(petID uuid.UUID, petName string, v app.CardView, version int, verifyBase string) CardDocument {
	hash := app.CardFingerprint(v.Entries)
	d := CardDocument{
		PetID: petID.String(), PetName: petName, Species: v.Species, Version: version,
		Entries: []cardDocEntry{}, NextDue: []cardDocDue{}, Hash: hash,
		VerifyURL: strings.TrimRight(verifyBase, "/") + "/" + petID.String() + "?h=" + hash,
	}
	for _, e := range v.Entries {
		if e.Status != "active" {
			continue
		}
		x := cardDocEntry{
			Date: e.AdministeredAt, Kind: cardKindLabels[e.Kind], Product: e.ProductName,
			Verification: cardVerificationLabels[e.Verification], Source: cardSourceLabels[e.Source],
		}
		if e.Lot != nil {
			x.Lot = *e.Lot
		}
		if e.DoseLabel != nil {
			x.Dose = *e.DoseLabel
		}
		d.Entries = append(d.Entries, x)
	}
	d.HasEntries = len(d.Entries) > 0
	for _, n := range v.NextDue {
		if n.Status != "upcoming" && n.Status != "overdue" {
			continue
		}
		d.NextDue = append(d.NextDue, cardDocDue{Product: n.ProductName, Due: n.DueDate, Status: cardDueLabels[n.Status]})
	}
	return d
}

// CardPDFKey es la clave del objeto del carnet (ARQ-010).
func CardPDFKey(pet uuid.UUID, version int) string {
	return fmt.Sprintf("clinical/%s/card/v%d.pdf", pet, version)
}

func (w *PDFWorker) renderCard(ctx context.Context, j pdfJob) (string, error) {
	card, err := NewCardQueries(w.pool).Card(ctx, j.ref, false)
	if err != nil {
		return "", err
	}
	var name string
	if err := w.pool.QueryRow(ctx, `SELECT name FROM read_petregistry.pets WHERE id = $1`, j.ref).Scan(&name); err != nil {
		return "", err
	}
	data := BuildCardDocument(j.ref, name, card, j.version, strings.Replace(w.verifyURL, "/verify/encounters/", "/verify/cards/", 1))
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	code, err := qr.Encode(data.VerifyURL, qr.M)
	if err != nil {
		return "", fmt.Errorf("qr: %w", err)
	}
	pdf, err := w.renderer.Render(ctx, cardTemplate, raw, pdfrender.File{Name: "qr.png", Data: code.PNG()})
	if err != nil {
		return "", err
	}
	key := CardPDFKey(j.ref, j.version)
	if err := w.objects.Put(ctx, key, "application/pdf", bytes.NewReader(pdf), int64(len(pdf))); err != nil {
		return "", err
	}
	return key, nil
}

// EnsureCardPDF devuelve el trabajo de la versión vigente del carnet,
// creándolo si no existe (la versión es la del stream: cualquier cambio
// genera un documento nuevo; las versiones previas quedan como caché).
func (d *DocumentStore) EnsureCardPDF(ctx context.Context, petID uuid.UUID) (app.DocumentJobView, error) {
	var version int
	if err := d.pool.QueryRow(ctx, `SELECT version::int FROM read_clinical.cards WHERE pet_id = $1`, petID).Scan(&version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.DocumentJobView{}, &kernel.NotFoundError{Resource: "card"}
		}
		return app.DocumentJobView{}, err
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO read_clinical.document_jobs (id, kind, ref_id, version, status)
		VALUES ($1, $2, $3, $4, 'pending') ON CONFLICT (kind, ref_id, version) DO NOTHING`,
		uuid.Must(uuid.NewV7()), app.DocumentJobKindCardPDF, petID, version); err != nil {
		return app.DocumentJobView{}, err
	}
	return scanDocument(d.pool.QueryRow(ctx, `SELECT `+documentColumns+`
		FROM read_clinical.document_jobs WHERE kind = $1 AND ref_id = $2 AND version = $3`,
		app.DocumentJobKindCardPDF, petID, version))
}
