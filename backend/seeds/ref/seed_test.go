package refseed

import (
	"context"
	"testing"
)

// Mientras APPROVALS.md esté pendiente, nada se activa; con la bandera de dev, todo.
func TestApply_ActiveFollowsApproval(t *testing.T) {
	for _, tc := range []struct {
		include bool
		active  bool
	}{{false, false}, {true, true}} {
		var rows, activeRows int
		n, err := Apply(context.Background(), func(_ context.Context, _ string, args ...any) error {
			rows++
			if got, _ := args[len(args)-1].(bool); got {
				activeRows++
			}
			return nil
		}, tc.include)
		if err != nil {
			t.Fatal(err)
		}
		if n != rows || n != 7+6+6 {
			t.Fatalf("filas = %d/%d, want 19", n, rows)
		}
		if (activeRows == rows) != tc.active || (!tc.active && activeRows != 0) {
			t.Errorf("include=%v: activas %d de %d", tc.include, activeRows, rows)
		}
	}
}

func TestVaccineID_Deterministic(t *testing.T) {
	if VaccineID("Rabia") != VaccineID("Rabia") || VaccineID("Rabia") == VaccineID("Triple felina (FVRCP)") {
		t.Fatal("ids no deterministas o colisionan")
	}
}
