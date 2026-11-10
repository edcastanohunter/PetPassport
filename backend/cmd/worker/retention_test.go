package main

import (
	"testing"
	"time"
)

func TestRetentionConstants_PP_F4_DAT_006(t *testing.T) {
	// Verificar que las constantes de retención cumplen exactamente PP-F4-DAT-006
	if partnerCallsRetention != 365*24*time.Hour {
		t.Fatalf("partnerCallsRetention debe ser 365 días (12 meses), obtenido: %v", partnerCallsRetention)
	}
	if webhookPayloadRetention != 7*24*time.Hour {
		t.Fatalf("webhookPayloadRetention debe ser 7 días, obtenido: %v", webhookPayloadRetention)
	}
	if webhookMetadataRetention != 180*24*time.Hour {
		t.Fatalf("webhookMetadataRetention debe ser 180 días, obtenido: %v", webhookMetadataRetention)
	}
	if extractionArtifactRetention != 90*24*time.Hour {
		t.Fatalf("extractionArtifactRetention debe ser 90 días, obtenido: %v", extractionArtifactRetention)
	}
	if orderPIIRetention != 180*24*time.Hour {
		t.Fatalf("orderPIIRetention debe ser 180 días, obtenido: %v", orderPIIRetention)
	}
}
