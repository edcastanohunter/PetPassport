package flags_test

import (
	"context"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/flags"
)

func TestFlags_OPS010_Evaluation(t *testing.T) {
	ctx := context.Background()

	store := flags.NewMemoryStore([]flags.Flag{
		{
			Key:               "feature_disabled",
			Enabled:           false,
			RolloutPercentage: 100,
			UpdatedAt:         time.Now(),
		},
		{
			Key:               "feature_full",
			Enabled:           true,
			RolloutPercentage: 100,
			UpdatedAt:         time.Now(),
		},
		{
			Key:               "feature_zero_rollout",
			Enabled:           true,
			RolloutPercentage: 0,
			UpdatedAt:         time.Now(),
		},
		{
			Key:               "feature_targeted_role",
			Enabled:           true,
			RolloutPercentage: 100,
			Targeting: map[string]any{
				"allowed_roles": []string{"veterinarian", "super_admin"},
			},
			UpdatedAt: time.Now(),
		},
		{
			Key:               "feature_percentage_50",
			Enabled:           true,
			RolloutPercentage: 50,
			UpdatedAt:         time.Now(),
		},
	})

	engine := flags.NewEngine(store)

	t.Run("disabled flag returns false always", func(t *testing.T) {
		res := engine.IsEnabled(ctx, "feature_disabled", flags.EvaluationContext{UserID: "usr_123"})
		if res {
			t.Errorf("expected disabled flag to be false")
		}
	})

	t.Run("fully enabled flag returns true", func(t *testing.T) {
		res := engine.IsEnabled(ctx, "feature_full", flags.EvaluationContext{UserID: "usr_123"})
		if !res {
			t.Errorf("expected 100%% enabled flag to be true")
		}
	})

	t.Run("zero percent rollout returns false", func(t *testing.T) {
		res := engine.IsEnabled(ctx, "feature_zero_rollout", flags.EvaluationContext{UserID: "usr_123"})
		if res {
			t.Errorf("expected 0%% rollout flag to be false")
		}
	})

	t.Run("role targeting allows authorized role and denies others", func(t *testing.T) {
		vetCtx := flags.EvaluationContext{UserID: "u1", Role: "veterinarian"}
		ownerCtx := flags.EvaluationContext{UserID: "u2", Role: "pet_owner"}

		if !engine.IsEnabled(ctx, "feature_targeted_role", vetCtx) {
			t.Errorf("expected veterinarian to have flag enabled")
		}
		if engine.IsEnabled(ctx, "feature_targeted_role", ownerCtx) {
			t.Errorf("expected pet_owner to have flag disabled")
		}
	})

	t.Run("deterministic percentage rollout is consistent for same user", func(t *testing.T) {
		u1 := flags.EvaluationContext{UserID: "consistent_user_1"}
		res1 := engine.IsEnabled(ctx, "feature_percentage_50", u1)
		res2 := engine.IsEnabled(ctx, "feature_percentage_50", u1)

		if res1 != res2 {
			t.Fatalf("expected deterministic result for user, got %v and %v", res1, res2)
		}
	})

	t.Run("AllFlags returns map of all flags evaluated", func(t *testing.T) {
		all := engine.AllFlags(ctx, flags.EvaluationContext{UserID: "u1", Role: "veterinarian"})
		if len(all) != 5 {
			t.Errorf("expected 5 flags, got %d", len(all))
		}
		if all["feature_disabled"] != false {
			t.Errorf("expected feature_disabled to be false")
		}
		if all["feature_full"] != true {
			t.Errorf("expected feature_full to be true")
		}
		if all["feature_targeted_role"] != true {
			t.Errorf("expected feature_targeted_role to be true for vet")
		}
	})
}
