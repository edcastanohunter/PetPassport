// Package testkit contiene el harness de pruebas de dominio Given/When/Then
// (PP-F1-QA-002, docs/phase-1/10 §2). Rehidrata el agregado aplicando los
// eventos de Given, ejecuta el comando de When y compara los eventos emitidos
// por tipo y payload (sin event_id ni occurred_at, que no existen en los
// eventos de dominio) o el error devuelto.
//
//	testkit.Scenario[*petregistry.Pet](t, func() *petregistry.Pet { return &petregistry.Pet{} }).
//		Given(petregistry.PetRegistered{...}, petregistry.PetMarkedDeceased{...}).
//		When(petregistry.AssignMicrochip{Microchip: "941000012345678"}).
//		ThenError(petregistry.ErrPetDeceased)
package testkit

import (
	"testing"
)

// Aggregate es el contrato que el harness exige al agregado bajo prueba.
// ApplyEvent muta el estado (la misma función que usa el repositorio al
// rehidratar desde el event store); HandleCommand decide y devuelve los
// eventos de dominio a persistir.
type Aggregate interface {
	ApplyEvent(evt any) error
	HandleCommand(cmd any) ([]any, error)
}

// Scenario construye el escenario de prueba.
func Scenario[A Aggregate](t testing.TB, newAgg func() A) *ScenarioCtx[A] {
	t.Helper()
	return &ScenarioCtx[A]{t: t, agg: newAgg()}
}

// ScenarioCtx es el builder del escenario.
type ScenarioCtx[A Aggregate] struct {
	t     testing.TB
	agg   A
	given []any
	when  any
}

// Given rehidrata el agregado aplicando estos eventos (en orden).
func (s *ScenarioCtx[A]) Given(events ...any) *ScenarioCtx[A] {
	s.given = append(s.given, events...)
	return s
}

// When ejecuta el comando.
func (s *ScenarioCtx[A]) When(cmd any) *ScenarioCtx[A] {
	s.when = cmd
	return s
}

// apply rehidrata con Given y ejecuta When.
func (s *ScenarioCtx[A]) run() ([]any, error) {
	s.t.Helper()
	for i, e := range s.given {
		if err := s.agg.ApplyEvent(e); err != nil {
			s.t.Fatalf("Given[%d] (%T): aplicar evento: %v", i, e, err)
		}
	}
	if s.when == nil {
		s.t.Fatal("When sin comando")
	}
	return s.agg.HandleCommand(s.when)
}

// Then exige los eventos esperados por tipo y payload, en orden. El estado
// resultante queda disponible en el agregado para aserciones adicionales.
func (s *ScenarioCtx[A]) Then(want ...any) *ScenarioCtx[A] {
	s.t.Helper()
	got, err := s.run()
	if err != nil {
		s.t.Fatalf("When(%T): error inesperado: %v", s.when, err)
	}
	if len(got) != len(want) {
		s.t.Fatalf("When(%T): got %d eventos, want %d (%v)", s.when, len(got), len(want), typeNames(got))
	}
	for i, w := range want {
		if !sameEvent(got[i], w) {
			s.t.Fatalf("When(%T): evento[%d]:\n got %+v (%T)\nwant %+v (%T)", s.when, i, got[i], got[i], w, w)
		}
	}
	return s
}

// ThenNoEvents exige que el comando no emita eventos (idempotencia).
func (s *ScenarioCtx[A]) ThenNoEvents() {
	s.t.Helper()
	got, err := s.run()
	if err != nil {
		s.t.Fatalf("When(%T): error inesperado: %v", s.when, err)
	}
	if len(got) != 0 {
		s.t.Fatalf("When(%T): se esperaban 0 eventos, got %v", s.when, typeNames(got))
	}
}

// ThenError exige que el comando falle con el error esperado. Coincide por
// errors.Is (centinelas de dominio) o por tipo (ValidationError y compañía).
func (s *ScenarioCtx[A]) ThenError(want error) *ScenarioCtx[A] {
	s.t.Helper()
	got, err := s.run()
	if err == nil {
		s.t.Fatalf("When(%T): se esperaba error %v, got eventos %v", s.when, want, typeNames(got))
	}
	if !errorIs(err, want) {
		s.t.Fatalf("When(%T):\n got error %v\nwant error %v", s.when, err, want)
	}
	return s
}

// Agg devuelve el agregado rehidratado tras ejecutar el escenario.
func (s *ScenarioCtx[A]) Agg() A { return s.agg }

func typeNames(events []any) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = typeName(e)
	}
	return out
}
