// Command worker es el proceso de proyecciones asíncronas, outbox y jobs.
// En F1 es un stub: documenta el contrato del consumidor (docs/phase-1/03 §4
// ARQ-006) y se implementa en F2 M2.0 (ADR-009).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "petpassport worker: no operativo en F1; se implementa en F2 M2.0 (ADR-009).")
	fmt.Fprintln(os.Stderr, "Contrato del consumidor de outbox: lectura por global_position con FOR UPDATE SKIP LOCKED,")
	fmt.Fprintln(os.Stderr, "publicación idempotente y marca de published_at; proyecciones por suscripción catch-up.")
	os.Exit(2)
}
