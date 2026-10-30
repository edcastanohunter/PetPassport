package domain

// Warning es un aviso de rango por especie (PRD-002): no bloquea el guardado,
// la UI lo muestra junto al campo (FE-003). `Code` es estable y traducible;
// `Field` usa el nombre del campo del contrato sin el prefijo `vitals.`.
type Warning struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Códigos de aviso (PRD-002).
const (
	WarnBelowSpeciesRange = "BELOW_SPECIES_RANGE"
	WarnAboveSpeciesRange = "ABOVE_SPECIES_RANGE"
)

type speciesRange struct {
	tempLo, tempHi           float64
	hrLo, hrHi               int
	rrLo, rrHi               int
	weightLo, weightHi       float64
	hasT, hasHR, hasRR, hasW bool
}

// speciesRanges son rangos de referencia veterinaria amplios: el objetivo es
// avisar de un valor probablemente erróneo, no diagnosticar. Un valor fuera
// de rango se guarda igualmente (PRD-002: "aviso, no bloqueo").
var speciesRanges = map[string]speciesRange{
	"dog":    {tempLo: 37.5, tempHi: 39.2, hrLo: 60, hrHi: 140, rrLo: 10, rrHi: 30, weightLo: 0.5, weightHi: 90, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"cat":    {tempLo: 37.8, tempHi: 39.5, hrLo: 140, hrHi: 220, rrLo: 20, rrHi: 40, weightLo: 0.5, weightHi: 12, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"rabbit": {tempLo: 38.0, tempHi: 39.5, hrLo: 130, hrHi: 300, rrLo: 30, rrHi: 60, weightLo: 0.3, weightHi: 8, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"equine": {tempLo: 37.0, tempHi: 38.5, hrLo: 28, hrHi: 44, rrLo: 8, rrHi: 16, weightLo: 50, weightHi: 1200, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"ferret": {tempLo: 37.8, tempHi: 39.5, hrLo: 180, hrHi: 250, rrLo: 30, rrHi: 40, weightLo: 0.4, weightHi: 3, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"rodent": {tempLo: 36.5, tempHi: 38.5, hrLo: 250, hrHi: 500, rrLo: 70, rrHi: 120, weightLo: 0.01, weightHi: 2, hasT: true, hasHR: true, hasRR: true, hasW: true},
	"bird":   {tempLo: 38.5, tempHi: 41.5, rrLo: 15, rrHi: 60, weightLo: 0.01, weightHi: 5, hasT: true, hasRR: true, hasW: true},
}

func floatWarning(field string, v *float64, lo, hi float64) *Warning {
	if v == nil {
		return nil
	}
	if *v < lo {
		return &Warning{Field: field, Code: WarnBelowSpeciesRange, Message: "Por debajo del rango habitual para la especie."}
	}
	if *v > hi {
		return &Warning{Field: field, Code: WarnAboveSpeciesRange, Message: "Por encima del rango habitual para la especie."}
	}
	return nil
}

func intWarning(field string, v *int, lo, hi int) *Warning {
	if v == nil {
		return nil
	}
	if *v < lo {
		return &Warning{Field: field, Code: WarnBelowSpeciesRange, Message: "Por debajo del rango habitual para la especie."}
	}
	if *v > hi {
		return &Warning{Field: field, Code: WarnAboveSpeciesRange, Message: "Por encima del rango habitual para la especie."}
	}
	return nil
}

// VitalsWarnings devuelve los avisos de rango por especie (PRD-002). Una
// especie sin tabla (reptile, fish, other) no genera avisos: no hay rangos de
// referencia fiables.
func VitalsWarnings(species string, v Vitals) []Warning {
	r, ok := speciesRanges[species]
	if !ok {
		return nil
	}
	out := make([]Warning, 0, 4)
	if r.hasT {
		if w := floatWarning("temp_c", v.TempC, r.tempLo, r.tempHi); w != nil {
			out = append(out, *w)
		}
	}
	if r.hasHR {
		if w := intWarning("heart_rate_bpm", v.HeartRateBpm, r.hrLo, r.hrHi); w != nil {
			out = append(out, *w)
		}
	}
	if r.hasRR {
		if w := intWarning("resp_rate_rpm", v.RespRateRpm, r.rrLo, r.rrHi); w != nil {
			out = append(out, *w)
		}
	}
	if r.hasW {
		if w := floatWarning("weight_kg", v.WeightKg, r.weightLo, r.weightHi); w != nil {
			out = append(out, *w)
		}
	}
	return out
}
