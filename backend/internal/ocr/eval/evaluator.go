package eval

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// Sample representa una muestra canónica de evaluación con su verdad terreno (ground truth).
type Sample struct {
	ID         string
	PageBytes  []byte
	Header     ocrdomain.ExtractedHeader
	Items      []ocrdomain.ExtractedItem
	Legibility float64
}

// BuildSyntheticDataset genera las muestras sintéticas con ground truth (PP-F4-OCR-007, QA-003).
// Contiene 65 recetas veterinarias sintéticas con diversidad de especies, medicamentos, vías y posologías.
func BuildSyntheticDataset() []Sample {
	var samples []Sample

	vets := []string{"Dr. Camilo Restrepo", "Dra. Valentina Gómez", "Dr. Andrés Morales", "Dra. Carolina Ríos", "Dr. Felipe Castro"}
	clinics := []string{"Clínica Veterinaria San Roque", "Hospital Veterinario Central", "Centro Médico Animal PetCare", "Veterinaria Los Andes"}
	speciesList := []string{"Canino", "Felino"}
	pets := []string{"Luna", "Max", "Toby", "Simba", "Milo", "Kira", "Zeus", "Nala", "Rocky", "Thor", "Maya", "Bruno", "Coco"}

	medications := []struct {
		product   string
		dose      string
		frequency string
		duration  string
		quantity  string
	}{
		{"Amoxicilina + Ácido Clavulánico 250mg", "250mg", "c/12h", "7 días", "1 frasco"},
		{"Meloxicam 0.5% Gotas", "3 gotas", "SID (cada 24h)", "5 días", "1 frasco"},
		{"Prednisolona 5mg Comprimidos", "5mg", "cada 24 horas", "10 días", "1 caja"},
		{"Metronidazol 250mg", "125mg", "BID (c/12h)", "7 días", "1 blíster"},
		{"Cefalexina 500mg", "250mg", "TID (c/8h)", "10 días", "2 cajas"},
		{"Enrofloxacina 50mg Tabletas", "50mg", "SID", "5 días", "1 caja"},
		{"Tramadol Gotas 50mg/ml", "4 gotas", "c/8h", "3 días", "1 frasco"},
		{"Omeprazol 10mg Cápsulas", "10mg", "cada 24 horas", "14 días", "1 caja"},
		{"Doxiciclina 100mg Comprimidos", "50mg", "c/12h", "21 días", "2 cajas"},
		{"Ketoprofeno 10mg", "10mg", "SID", "4 días", "1 blíster"},
		{"Marbofloxacina 20mg", "20mg", "SID", "7 días", "1 caja"},
		{"Tobramicina Colirio 0.3%", "1 gota", "c/6h", "7 días", "1 frasco"},
		{"Ciprofloxacina Gotas Óticas", "3 gotas", "BID", "10 días", "1 frasco"},
	}

	sampleID := 1
	for _, med := range medications {
		for vIdx, vet := range vets {
			pet := pets[(sampleID)%len(pets)]
			clinic := clinics[(sampleID)%len(clinics)]
			species := speciesList[(sampleID)%len(speciesList)]

			sample := Sample{
				ID:         fmt.Sprintf("sample-syn-%03d", sampleID),
				PageBytes:  []byte(fmt.Sprintf("SYNTHETIC_RX_%03d_%s_%s", sampleID, med.product, pet)),
				Legibility: 0.90,
				Header: ocrdomain.ExtractedHeader{
					VetName: ptr(vet),
					Clinic:  ptr(clinic),
					PetName: ptr(pet),
					Species: ptr(species),
				},
				Items: []ocrdomain.ExtractedItem{
					{
						Index:   0,
						RawText: fmt.Sprintf("%s, %s, %s x %s. Cantidad: %s", med.product, med.dose, med.frequency, med.duration, med.quantity),
						Product: ocrdomain.ExtractedProduct{
							Text: med.product,
						},
						Dose:      med.dose,
						Frequency: med.frequency,
						Duration:  med.duration,
						Quantity:  med.quantity,
					},
				},
			}
			samples = append(samples, sample)
			sampleID++
			if sampleID > 65 {
				break
			}
			_ = vIdx
		}
		if len(samples) >= 65 {
			break
		}
	}

	return samples
}

// Evaluator ejecuta la evaluación continua del extractor (PP-F4-OCR-007, QA-003).
type Evaluator struct {
	extractor ocrdomain.DocumentExtractor
}

func NewEvaluator(ext ocrdomain.DocumentExtractor) *Evaluator {
	return &Evaluator{extractor: ext}
}

// Evaluate dataset y computa métricas canónicas por campo y F1.
func (e *Evaluator) Evaluate(ctx context.Context, dataset []Sample) (ocrdomain.EvalMetrics, error) {
	if len(dataset) == 0 {
		return ocrdomain.EvalMetrics{}, fmt.Errorf("dataset vacío")
	}

	var totalLatency int
	var totalCost float64
	var reuploadCount int

	var tpProduct, fpProduct, fnProduct int
	var tpDose, fpDose, fnDose int
	var tpFreq, fpFreq, fnFreq int
	var tpDur, fpDur, fnDur int
	var tpQty, fpQty, fnQty int
	var headerExactCount int

	for _, sample := range dataset {
		pages := []ocrdomain.PreprocessedPage{
			{
				PageNumber:   1,
				Data:         sample.PageBytes,
				ContentType:  "image/jpeg",
				QualityScore: sample.Legibility,
				Sha256:       []byte(sample.ID),
			},
		}

		start := time.Now()
		res, err := e.extractor.Extract(ctx, pages, ocrdomain.ExtractorHints{
			PetSpecies: val(sample.Header.Species),
		})
		latency := int(time.Since(start).Milliseconds())

		if err != nil {
			reuploadCount++
			fnProduct += len(sample.Items)
			fnDose += len(sample.Items)
			fnFreq += len(sample.Items)
			fnDur += len(sample.Items)
			fnQty += len(sample.Items)
			continue
		}

		totalLatency += latency
		totalCost += res.CostUSD

		if res.Extraction.Legibility < 0.40 {
			reuploadCount++
		}

		// Comparar Cabecera
		if normalizeStr(val(res.Extraction.Header.PetName)) == normalizeStr(val(sample.Header.PetName)) &&
			normalizeStr(val(res.Extraction.Header.VetName)) == normalizeStr(val(sample.Header.VetName)) {
			headerExactCount++
		}

		// Comparar Ítems
		for idx, gtItem := range sample.Items {
			if idx >= len(res.Extraction.Items) {
				fnProduct++
				fnDose++
				fnFreq++
				fnDur++
				fnQty++
				continue
			}

			pred := res.Extraction.Items[idx]

			// Producto
			if matchTokens(gtItem.Product.Text, pred.Product.Text) {
				tpProduct++
			} else {
				fpProduct++
				fnProduct++
			}

			// Dosis
			if matchTokens(gtItem.Dose, pred.Dose) {
				tpDose++
			} else {
				fpDose++
				fnDose++
			}

			// Frecuencia
			if matchTokens(gtItem.Frequency, pred.Frequency) {
				tpFreq++
			} else {
				fpFreq++
				fnFreq++
			}

			// Duración
			if matchTokens(gtItem.Duration, pred.Duration) {
				tpDur++
			} else {
				fpDur++
				fnDur++
			}

			// Cantidad
			if matchTokens(gtItem.Quantity, pred.Quantity) {
				tpQty++
			} else {
				fpQty++
				fnQty++
			}
		}

		// Extra items predichos falsamente
		if len(res.Extraction.Items) > len(sample.Items) {
			extra := len(res.Extraction.Items) - len(sample.Items)
			fpProduct += extra
			fpDose += extra
		}
	}

	f1Prod := computeF1(tpProduct, fpProduct, fnProduct)
	f1Dose := computeF1(tpDose, fpDose, fnDose)
	f1Freq := computeF1(tpFreq, fpFreq, fnFreq)
	f1Dur := computeF1(tpDur, fpDur, fnDur)
	f1Qty := computeF1(tpQty, fpQty, fnQty)

	headerMatch := float64(headerExactCount) / float64(len(dataset))
	reuploadRate := float64(reuploadCount) / float64(len(dataset))
	avgLatency := totalLatency / len(dataset)
	avgCost := totalCost / float64(len(dataset))

	return ocrdomain.EvalMetrics{
		TotalSamples:     len(dataset),
		F1Product:        f1Prod,
		F1Dose:           f1Dose,
		F1Frequency:      f1Freq,
		F1Duration:       f1Dur,
		F1Quantity:       f1Qty,
		HeaderExactMatch: headerMatch,
		ReuploadRate:     reuploadRate,
		AvgLatencyMs:     avgLatency,
		AvgCostUSD:       avgCost,
		FieldMetrics: map[string]float64{
			"product":   f1Prod,
			"dose":      f1Dose,
			"frequency": f1Freq,
			"duration":  f1Dur,
			"quantity":  f1Qty,
		},
	}, nil
}

func computeF1(tp, fp, fn int) float64 {
	precision := 0.0
	if tp+fp > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	recall := 0.0
	if tp+fn > 0 {
		recall = float64(tp) / float64(tp+fn)
	}
	if precision+recall == 0 {
		return 0.0
	}
	f1 := 2 * (precision * recall) / (precision + recall)
	return math.Round(f1*1000) / 1000.0
}

func matchTokens(a, b string) bool {
	na := normalizeStr(a)
	nb := normalizeStr(b)
	if na == nb {
		return true
	}
	if strings.Contains(na, nb) || strings.Contains(nb, na) {
		return true
	}
	wordsA := strings.Fields(na)
	wordsB := strings.Fields(nb)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}
	matches := 0
	for _, wa := range wordsA {
		for _, wb := range wordsB {
			if wa == wb {
				matches++
				break
			}
		}
	}
	return float64(matches)/float64(len(wordsA)) >= 0.50
}

func normalizeStr(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(".", "", ",", "", "+", " ", "/", " ", "(", "", ")", "", "-", " ")
	return replacer.Replace(s)
}

func ptr[T any](v T) *T { return &v }
func val(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
