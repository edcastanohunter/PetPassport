// Command gen-validation-fixtures serializa los casos de validación de
// backend/internal/petregistry/domain/validationcases al fixture compartido
// con la SPA (PP-F1-FE-011). Ejecutar desde backend/:
//
//	go run ./tools/gen-validation-fixtures > ../docs/phase-1/fixtures/validation-cases.json
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain/validationcases"
)

func main() {
	raw, err := json.MarshalIndent(validationcases.All(), "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(raw))
}
