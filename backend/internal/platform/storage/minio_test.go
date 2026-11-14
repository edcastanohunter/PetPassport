package storage

import (
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// PP-F5-ARQ-003 / CMP-009: los documentos fiscales se escriben con retención
// COMPLIANCE de 10 años; el resto del almacenamiento no lleva retención.
func TestPutOptions_PP_F5_ARQ_003_FiscalObjectsAreLocked(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	tenYears := 3653 * 24 * time.Hour

	locked := (&MinIO{retainFor: tenYears, now: func() time.Time { return now }}).putOptions("application/pdf")
	if locked.Mode != minio.Compliance || !locked.RetainUntilDate.Equal(now.Add(tenYears)) || !locked.SendContentMd5 {
		t.Fatalf("opciones con retención = mode %q hasta %s md5 %v", locked.Mode, locked.RetainUntilDate, locked.SendContentMd5)
	}
	if locked.RetainUntilDate.Year() != 2036 {
		t.Fatalf("la retención debe llegar a 2036, llega a %d", locked.RetainUntilDate.Year())
	}

	plain := (&MinIO{now: func() time.Time { return now }}).putOptions("image/webp")
	if plain.Mode != "" || !plain.RetainUntilDate.IsZero() || plain.ContentType != "image/webp" {
		t.Fatalf("media sin retención = %+v", plain)
	}
}
