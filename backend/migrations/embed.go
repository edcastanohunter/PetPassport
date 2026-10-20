// Package migrations embebe las migraciones SQL (goose) para cmd/migrate y
// para el testkit de integración.
package migrations

import "embed"

// FS contiene los archivos *.sql.
//
//go:embed *.sql
var FS embed.FS
