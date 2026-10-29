package domain

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// SeriesDose es una dosis de la serie inicial de un protocolo (DOM-009).
// MinAgeWeeks es la edad mínima del animal para esa dosis; MinIntervalDays
// el intervalo mínimo desde la dosis anterior (0 en la primera).
type SeriesDose struct {
	MinAgeWeeks     int `json:"min_age_weeks"`
	MinIntervalDays int `json:"min_interval_days"`
}

// Protocol es el subconjunto de VaccinationProtocol que usa el cálculo. Lo
// carga app desde ref.vaccination_protocols (país y especie ya resueltos).
type Protocol struct {
	VaccineID             uuid.UUID
	InitialSeries         []SeriesDose
	BoosterIntervalMonths *int
}

// Proposal es el resultado del cálculo con su explicación para la UI
// (API-004 /next-due/proposal).
type Proposal struct {
	NextDue     *NextDue
	Explanation string
}

// NextDueCalculator es el servicio de dominio de DOM-009. Sin estado.
type NextDueCalculator struct{}

// Propose calcula la próxima dosis de la entrada que se va a registrar.
//
//  1. Cuenta las dosis activas previas del mismo VaccineID (recorded o
//     verified; las declaradas sin verificar cuentan pero degradan Basis a
//     guardian; las disputadas no cuentan).
//  2. Si faltan dosis de la serie inicial: max(fecha + MinIntervalDays,
//     nacimiento + MinAgeWeeks de la siguiente dosis).
//  3. Si la serie está completa: fecha + BoosterIntervalMonths.
//  4. Sin protocolo (o sin refuerzo definido): nil, la clínica lo indica.
func (NextDueCalculator) Propose(entries []CardEntry, vaccineID uuid.UUID, administeredAt kernel.CivilDate,
	birth *kernel.CivilDate, p *Protocol) Proposal {
	if p == nil || p.VaccineID != vaccineID || administeredAt == "" {
		return Proposal{Explanation: "Sin protocolo para esta vacuna: indique la próxima dosis manualmente."}
	}
	basis := BasisProtocol
	prior := 0
	for _, e := range entries {
		if e.Status != EntryActive || e.Kind != KindVaccine || e.Product.CatalogID == nil || *e.Product.CatalogID != vaccineID {
			continue
		}
		if e.AdministeredAt.After(administeredAt) || e.Verification == Disputed {
			continue
		}
		prior++
		if e.Verification == Unverified {
			basis = BasisGuardian
		}
	}
	given := prior + 1 // contando la que se registra
	if given < len(p.InitialSeries) {
		next := p.InitialSeries[given]
		date := administeredAt.AddDays(next.MinIntervalDays)
		if birth != nil && next.MinAgeWeeks > 0 {
			if byAge := birth.AddDays(next.MinAgeWeeks * 7); byAge.After(date) {
				date = byAge
			}
		}
		return Proposal{
			NextDue:     &NextDue{Date: date, Basis: basis},
			Explanation: fmt.Sprintf("Dosis %d de %d de la serie inicial.", given+1, len(p.InitialSeries)),
		}
	}
	if p.BoosterIntervalMonths == nil || *p.BoosterIntervalMonths <= 0 {
		return Proposal{Explanation: "Serie completa y sin refuerzo definido en el protocolo."}
	}
	date := kernel.CivilDateOf(administeredAt.Time().AddDate(0, *p.BoosterIntervalMonths, 0))
	return Proposal{
		NextDue:     &NextDue{Date: date, Basis: basis},
		Explanation: fmt.Sprintf("Refuerzo a los %d meses.", *p.BoosterIntervalMonths),
	}
}
