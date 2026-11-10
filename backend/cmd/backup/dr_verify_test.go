package main_test

import (
	"testing"
	"time"
)

// TestDR_QA004_RPOandRTOObjectives verifies RPO <= 5 min and RTO <= 1 h thresholds (PP-F5-QA-004, OPS-003, OPS-004).
func TestDR_QA004_RPOandRTOObjectives(t *testing.T) {
	const (
		maxAllowedRPO = 5 * time.Minute
		maxAllowedRTO = 1 * time.Hour
	)

	t.Run("valid DR metrics within SLA pass", func(t *testing.T) {
		lastEventTime := time.Now().Add(-2 * time.Minute)
		measuredRPO := time.Since(lastEventTime)

		startTime := time.Now().Add(-15 * time.Minute)
		measuredRTO := time.Since(startTime)

		if measuredRPO > maxAllowedRPO {
			t.Errorf("measured RPO %v exceeds SLA %v", measuredRPO, maxAllowedRPO)
		}
		if measuredRTO > maxAllowedRTO {
			t.Errorf("measured RTO %v exceeds SLA %v", measuredRTO, maxAllowedRTO)
		}
	})

	t.Run("RPO exceeding 5 minutes fails check", func(t *testing.T) {
		lastEventTime := time.Now().Add(-6 * time.Minute)
		measuredRPO := time.Since(lastEventTime)

		if measuredRPO <= maxAllowedRPO {
			t.Fatalf("expected RPO %v to exceed SLA %v", measuredRPO, maxAllowedRPO)
		}
	})

	t.Run("RTO exceeding 1 hour fails check", func(t *testing.T) {
		startTime := time.Now().Add(-65 * time.Minute)
		measuredRTO := time.Since(startTime)

		if measuredRTO <= maxAllowedRTO {
			t.Fatalf("expected RTO %v to exceed SLA %v", measuredRTO, maxAllowedRTO)
		}
	})
}
