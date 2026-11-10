package entitlements

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	partnerdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type consumingBusCommand interface {
	kernel.ConsumingCommand
	bus.Command
}

// TestCommandLint_ConsumesDeclaration verifica por reflexión que los comandos que consumen recursos
// o capacidades de plan implementan kernel.ConsumingCommand (PP-F5-ARQ-004).
func TestCommandLint_ConsumesDeclaration(t *testing.T) {
	consumingCommands := []any{
		partnerdomain.InviteMember{OrganizationID: uuid.New(), Role: "veterinarian"},
		partnerdomain.UpdateDirectoryListing{OrganizationID: uuid.New(), Listed: true},
	}

	consumingType := reflect.TypeOf((*kernel.ConsumingCommand)(nil)).Elem()

	for _, cmd := range consumingCommands {
		cmdType := reflect.TypeOf(cmd)
		if !cmdType.Implements(consumingType) {
			t.Errorf("comando %s debe implementar kernel.ConsumingCommand", cmdType.Name())
			continue
		}

		cc := cmd.(kernel.ConsumingCommand)
		req := cc.Consumes()
		if req.Kind == "" {
			t.Errorf("comando %s devolvió un Requirement sin Kind", cmdType.Name())
		}
		if cc.TargetOrgID() == uuid.Nil {
			t.Errorf("comando %s devolvió un TargetOrgID nulo", cmdType.Name())
		}
	}
}

// TestCommandMatrix_PlansVsRequirements evalúa la matriz de planes vs comandos consumidores (ARQ-004).
func TestCommandMatrix_PlansVsRequirements(t *testing.T) {
	starter := kernel.EntitlementSet{
		PlanCode: "starter",
		Status:   kernel.EntitlementActive,
		Seats:    kernel.SeatAllocation{Limit: 2, Used: 2},
		Quotas: map[kernel.QuotaKey]kernel.Quota{
			kernel.QuotaQuotesPerMonth: {Limit: 50, Used: 50, SoftLimit: 55},
		},
		Features:   []string{"agenda", "encounters", "prescriptions"},
		ComputedAt: time.Now(),
	}

	pro := kernel.EntitlementSet{
		PlanCode: "pro",
		Status:   kernel.EntitlementActive,
		Seats:    kernel.SeatAllocation{Limit: 10, Used: 2},
		Quotas: map[kernel.QuotaKey]kernel.Quota{
			kernel.QuotaQuotesPerMonth: {Limit: 500, Used: 50, SoftLimit: 550},
		},
		Features:   []string{"agenda", "encounters", "prescriptions", "directory", "cohorts"},
		ComputedAt: time.Now(),
	}

	tests := []struct {
		name      string
		plan      kernel.EntitlementSet
		cmd       consumingBusCommand
		expectErr bool
	}{
		{
			name:      "Starter no puede invitar más veterinarios si alcanzó el límite de 2",
			plan:      starter,
			cmd:       partnerdomain.InviteMember{OrganizationID: uuid.New(), Role: "veterinarian"},
			expectErr: true,
		},
		{
			name:      "Starter puede invitar recepcionista sin consumir asiento",
			plan:      starter,
			cmd:       partnerdomain.InviteMember{OrganizationID: uuid.New(), Role: "receptionist"},
			expectErr: false,
		},
		{
			name:      "Pro puede invitar veterinarios si tiene asientos disponibles",
			plan:      pro,
			cmd:       partnerdomain.InviteMember{OrganizationID: uuid.New(), Role: "veterinarian"},
			expectErr: false,
		},
		{
			name:      "Starter no tiene capacidad 'directory'",
			plan:      starter,
			cmd:       partnerdomain.UpdateDirectoryListing{OrganizationID: uuid.New(), Listed: true},
			expectErr: true,
		},
		{
			name:      "Pro tiene capacidad 'directory'",
			plan:      pro,
			cmd:       partnerdomain.UpdateDirectoryListing{OrganizationID: uuid.New(), Listed: true},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := kernel.NewFakeEntitlements()
			orgID := tc.cmd.TargetOrgID()
			planCopy := tc.plan
			planCopy.OrgID = orgID
			fake.Set(orgID, planCopy)

			mw := EnforceEntitlements(fake)
			handler := mw(func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
				return bus.Result{}, nil
			})

			_, err := handler(context.Background(), tc.cmd)
			if tc.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
