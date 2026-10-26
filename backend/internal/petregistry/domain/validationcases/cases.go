// Package validationcases es la única fuente de los casos de validación que
// se comparten con el frontend vía docs/phase-1/fixtures/validation-cases.json
// (PP-F1-FE-011). El generador tools/gen-validation-fixtures lo serializa y
// los tests zod de la SPA ejecutan los mismos casos. Puro: sin dependencias.
package validationcases

// FieldCases agrupa los casos válidos e inválidos de un VO.
type FieldCases struct {
	Valid   []string `json:"valid"`
	Invalid []string `json:"invalid"`
}

// DateCase es un caso de fecha civil con su "hoy" de referencia.
type DateCase struct {
	Date        string `json:"date"`
	Today       string `json:"today"`
	Approximate bool   `json:"approximate"`
	Valid       bool   `json:"valid"`
}

// Fixture es el documento completo del fixture.
type Fixture struct {
	Version   string                `json:"version"`
	Generated string                `json:"generated_from"`
	Fields    map[string]FieldCases `json:"fields"`
	Dates     map[string][]DateCase `json:"dates"`
}

// All devuelve los casos de F1 para los VOs de mascotas (DOM-004).
func All() Fixture {
	return Fixture{
		Version:   "1",
		Generated: "backend/internal/petregistry/domain (DOM-004)",
		Fields: map[string]FieldCases{
			"petName": {
				Valid:   []string{"Luna", "  Rex  ", "O'Brien", "María José", "a", "Rex Jr"},
				Invalid: []string{"", "   ", string(repeat('x', 61)), "Luna<script>", "Luna\t"},
			},
			"microchip": {
				Valid:   []string{"941000012345678", "941 0000 1234 567 8", "941-00001234567-8", "ABC123DEF", "abc123defg"},
				Invalid: []string{"", "94100001234567", "12345678", "9410000123456789X"},
			},
			"species": {
				Valid:   []string{"dog", "Dog", " cat ", "other"},
				Invalid: []string{"dragon", ""},
			},
			"sex": {
				Valid:   []string{"male", "female", "unknown"},
				Invalid: []string{"n/a", ""},
			},
			"neutered": {
				Valid:   []string{"yes", "no", "unknown"},
				Invalid: []string{"maybe", ""},
			},
			"breed": {
				Valid:   []string{"Beagle", " Mestizo "},
				Invalid: []string{"", string(repeat('b', 81))},
			},
			"markings": {
				Valid:   []string{"blanca", "tricolor"},
				Invalid: []string{string(repeat('m', 201))},
			},
			"publicCode": {
				Valid:   []string{"7K3MQ2", "0189AB"},
				Invalid: []string{"7K3MQ", "7K3MQ23", "7K3MQI", "7k3mq2"},
			},
			// TaxID (DOM-007, M1.3): CO = NIT con dígito de verificación DIAN;
			// el resto de países usa el validador genérico. Los casos se
			// ejecutan contra partnernetwork/domain.NewTaxID leyendo el fixture.
			"taxIdCO": {
				Valid:   []string{"900123456-8", "900.123.456-8", "800000001-1", " 860001022-7 "},
				Invalid: []string{"900123456-7", "900123456", "900123456-X", "9001A3456-8", ""},
			},
			"taxIdGeneric": {
				Valid:   []string{"ABC123456", "b-12345678", "RFC.123-45"},
				Invalid: []string{"AB1", "ABC_12345", "ABCDEFGHIJKLMNOPQRSTU", ""},
			},
		},
		Dates: map[string][]DateCase{
			"birthDate": {
				{Date: "2021-05-10", Today: "2026-10-20", Valid: true},
				{Date: "2026-10-20", Today: "2026-10-20", Valid: true},
				{Date: "2026-10-21", Today: "2026-10-20", Valid: false},
				{Date: "2021-5-10", Today: "2026-10-20", Valid: false},
				{Date: "1985-01-01", Today: "2026-10-20", Valid: false},
				{Date: "2021-05-10", Today: "2026-10-20", Approximate: true, Valid: true},
			},
		},
	}
}

func repeat(r byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = r
	}
	return out
}
