package storage

import (
	"context"
	"testing"
	"time"

	"watchdog-agent/internal/model"
)

func TestVerifications(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("initialize store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	t0 := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	if v, err := store.GetVerification(ctx, 7); err != nil || v != nil {
		t.Fatalf("missing verification: %v, %v", v, err)
	}
	for _, v := range []*model.Verification{
		{PR: 7, Target: "default/yolo-detector", MergedAt: t0, Changes: `{"cpu":"350m"}`, Status: model.VerificationWaiting},
		{PR: 8, Target: "default/flask-calc", MergedAt: t0.Add(time.Hour), Status: model.VerificationSkipped},
	} {
		if err := store.SaveVerification(ctx, v); err != nil {
			t.Fatalf("save #%d: %v", v.PR, err)
		}
	}
	deployed := t0.Add(2 * time.Minute)
	update := &model.Verification{PR: 7, Target: "default/yolo-detector", MergedAt: t0, Changes: `{"cpu":"350m"}`,
		Status: model.VerificationVerifying, DeployedAt: &deployed, Baseline: &model.HealthSignals{Restarts: 1, Availability: 1}}
	if err := store.SaveVerification(ctx, update); err != nil {
		t.Fatalf("update #7: %v", err)
	}

	got, err := store.GetVerification(ctx, 7)
	if err != nil || got.Status != model.VerificationVerifying || got.Baseline == nil || got.Baseline.Restarts != 1 || !got.DeployedAt.Equal(deployed) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	list, err := store.GetVerifications(ctx, 10)
	if err != nil || len(list) != 2 || list[0].PR != 8 || list[1].PR != 7 {
		t.Fatalf("want #8 then #7, got %+v, %v", list, err)
	}

	halted := []model.HaltedWorkload{{Target: "default/yolo-detector", Since: t0, FailedPRs: []int{3, 4}}}
	if err := store.SetHaltedWorkloads(ctx, halted); err != nil {
		t.Fatalf("set halted: %v", err)
	}
	if got, err := store.GetHaltedWorkloads(ctx); err != nil || len(got) != 1 || len(got[0].FailedPRs) != 2 {
		t.Fatalf("halted round trip: %+v, %v", got, err)
	}
	if err := store.SetHaltedWorkloads(ctx, nil); err != nil {
		t.Fatalf("clear halted: %v", err)
	}
	if got, err := store.GetHaltedWorkloads(ctx); err != nil || len(got) != 0 {
		t.Fatalf("want no halted workloads, got %+v, %v", got, err)
	}
}
