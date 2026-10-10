package verify

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/gitops"
	"watchdog-agent/internal/model"
	"watchdog-agent/internal/storage"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type fakeChanges struct {
	merged     []gitops.MergedChange
	rollbackPR int
	rollbacks  []rollbackCall
}

type rollbackCall struct {
	target   string
	restore  gitops.ChangeSet
	source   int
	findings []string
}

func (f *fakeChanges) MergedChanges(context.Context, time.Time) ([]gitops.MergedChange, error) {
	return f.merged, nil
}

func (f *fakeChanges) OpenRollback(_ context.Context, target string, restore gitops.ChangeSet, source int, findings []string) (int, error) {
	f.rollbacks = append(f.rollbacks, rollbackCall{target, restore, source, findings})
	return f.rollbackPR, nil
}

type window struct{ start, end time.Time }

type fakeMetrics struct {
	signals func(start, end time.Time) model.HealthSignals
	calls   []window
}

func (f *fakeMetrics) HealthSignals(_ context.Context, _, _ string, start, end time.Time) (model.HealthSignals, error) {
	f.calls = append(f.calls, window{start, end})
	return f.signals(start, end), nil
}

type fakeCluster struct{ deps map[string]*appsv1.Deployment }

func (f *fakeCluster) GetDeployment(_ context.Context, ns, name string) (*appsv1.Deployment, error) {
	if d, ok := f.deps[ns+"/"+name]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

// deployment builds a single-container Deployment with the given CPU request, fully rolled out.
func deployment(cpu string, annotations map[string]string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 2, Annotations: annotations},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)}},
			}}}},
		},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
}

func cpuChange(pr int, cpu, prev string, mergedAt time.Time) gitops.MergedChange {
	mc := gitops.MergedChange{PR: pr, URL: "https://example/pr", Target: "default/yolo-detector", MergedAt: mergedAt,
		Changes: gitops.ChangeSet{CPU: cpu}}
	if prev != "" {
		mc.Previous = &gitops.ChangeSet{CPU: prev}
	}
	return mc
}

type harness struct {
	v       *Verifier
	changes *fakeChanges
	metrics *fakeMetrics
	cluster *fakeCluster
	store   storage.Store
	now     time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	h := &harness{
		changes: &fakeChanges{rollbackPR: 9},
		metrics: &fakeMetrics{signals: func(time.Time, time.Time) model.HealthSignals {
			return model.HealthSignals{AvgReplicas: 1, Availability: 1}
		}},
		cluster: &fakeCluster{deps: map[string]*appsv1.Deployment{"default/yolo-detector": deployment("500m", nil)}},
		store:   store,
		now:     t0,
	}
	cfg := config.VerificationConfig{Enabled: true, Window: "30m", RolloutTimeout: "30m", Lookback: "168h",
		MaxRollbacks: 2, MaxThrottleIncrease: 0.10, MaxReplicaIncrease: 0.5, MinAvailability: 0.9}
	h.v, err = New(cfg, h.changes, h.metrics, h.cluster, store)
	if err != nil {
		t.Fatal(err)
	}
	h.v.now = func() time.Time { return h.now }
	return h
}

func (h *harness) run(t *testing.T, after time.Duration) map[string]model.HaltedWorkload {
	t.Helper()
	h.now = h.now.Add(after)
	return h.v.Run(context.Background())
}

func (h *harness) get(t *testing.T, pr int) *model.Verification {
	t.Helper()
	v, err := h.store.GetVerification(context.Background(), pr)
	if err != nil || v == nil {
		t.Fatalf("verification #%d: %v, %v", pr, v, err)
	}
	return v
}

func TestVerifiedChange(t *testing.T) {
	h := newHarness(t)
	h.changes.merged = []gitops.MergedChange{cpuChange(7, "350m", "500m", t0)}

	h.run(t, time.Minute)
	if got := h.get(t, 7); got.Status != model.VerificationWaiting {
		t.Fatalf("before rollout: status %s", got.Status)
	}

	h.cluster.deps["default/yolo-detector"] = deployment("350m", nil)
	h.run(t, time.Minute)
	got := h.get(t, 7)
	if got.Status != model.VerificationVerifying || got.DeployedAt == nil || got.Baseline == nil {
		t.Fatalf("after rollout: %+v", got)
	}
	if w := h.metrics.calls[0]; !w.end.Equal(t0) || !w.start.Equal(t0.Add(-30*time.Minute)) {
		t.Errorf("baseline window %v, want the 30m before the merge", w)
	}

	h.run(t, 10*time.Minute)
	if got := h.get(t, 7); got.Status != model.VerificationVerifying || got.Observed == nil {
		t.Fatalf("mid-window: %+v", got)
	}

	h.run(t, 25*time.Minute)
	if got := h.get(t, 7); got.Status != model.VerificationVerified {
		t.Fatalf("after window: %+v", got)
	}
	if len(h.changes.rollbacks) != 0 {
		t.Error("a healthy change must not be rolled back")
	}
}

func TestDegradedChangeIsRolledBack(t *testing.T) {
	h := newHarness(t)
	h.changes.merged = []gitops.MergedChange{cpuChange(7, "350m", "500m", t0)}
	h.cluster.deps["default/yolo-detector"] = deployment("350m", nil)
	h.run(t, time.Minute) // rolled out: verifying

	deployed := h.now
	h.metrics.signals = func(start, end time.Time) model.HealthSignals {
		s := model.HealthSignals{AvgReplicas: 1, Availability: 1}
		if !start.Before(deployed) {
			s.Restarts, s.OOMKills = 3, 1
		}
		return s
	}
	h.run(t, 5*time.Minute) // restarts are caught before the window ends
	got := h.get(t, 7)
	if got.Status != model.VerificationDegraded || len(got.Findings) != 2 ||
		!strings.Contains(got.Findings[0], "3 container restarts") || !strings.Contains(got.Findings[1], "OOMKilled containers after the change: 1") {
		t.Fatalf("want Degraded with restart and OOM findings, got %+v", got)
	}

	h.run(t, time.Minute)
	got = h.get(t, 7)
	if got.Status != model.VerificationRollbackPR || got.RollbackPR != 9 {
		t.Fatalf("want a rollback PR, got %+v", got)
	}
	if len(h.changes.rollbacks) != 1 {
		t.Fatalf("want one rollback call, got %d", len(h.changes.rollbacks))
	}
	call := h.changes.rollbacks[0]
	if call.target != "default/yolo-detector" || call.restore.CPU != "500m" || call.source != 7 || len(call.findings) != 2 {
		t.Errorf("unexpected rollback call %+v", call)
	}

	h.run(t, time.Minute)
	if len(h.changes.rollbacks) != 1 {
		t.Error("the rollback must be proposed only once")
	}

	rollback := cpuChange(9, "500m", "350m", h.now)
	rollback.RollbackOf = 7
	h.changes.merged = append(h.changes.merged, rollback)
	h.run(t, time.Minute)
	if got := h.get(t, 7); got.Status != model.VerificationRolledBack || got.RollbackPR != 9 {
		t.Fatalf("want RolledBack, got %+v", got)
	}
	if v, _ := h.store.GetVerification(context.Background(), 9); v != nil {
		t.Error("a rollback PR must not be verified itself")
	}
}

func TestAveragedSignalsWaitForTheWindow(t *testing.T) {
	h := newHarness(t)
	h.changes.merged = []gitops.MergedChange{cpuChange(7, "350m", "500m", t0)}
	h.cluster.deps["default/yolo-detector"] = deployment("350m", nil)
	h.metrics.signals = func(start, end time.Time) model.HealthSignals {
		if !end.After(t0) { // the baseline window ends at the merge
			return model.HealthSignals{ThrottleRatio: 0.02, AvgReplicas: 1, Availability: 1}
		}
		return model.HealthSignals{ThrottleRatio: 0.30, AvgReplicas: 2, Availability: 1}
	}
	h.run(t, time.Minute)
	h.run(t, 10*time.Minute)
	if got := h.get(t, 7); got.Status != model.VerificationVerifying {
		t.Fatalf("throttling must not fail the change mid-window: %+v", got)
	}
	h.run(t, 25*time.Minute)
	got := h.get(t, 7)
	if got.Status != model.VerificationDegraded || len(got.Findings) != 2 ||
		!strings.Contains(got.Findings[0], "throttling rose from 2% to 30%") || !strings.Contains(got.Findings[1], "replicas rose from 1.0 to 2.0") {
		t.Fatalf("want throttling and replica findings, got %+v", got)
	}
}

func TestUnverifiableChanges(t *testing.T) {
	h := newHarness(t)
	h.changes.merged = []gitops.MergedChange{
		cpuChange(5, "350m", "", t0),     // merged before PRs recorded previous values
		cpuChange(6, "300m", "500m", t0), // never reaches the cluster
	}
	h.run(t, time.Minute)
	if got := h.get(t, 5); got.Status != model.VerificationSkipped {
		t.Errorf("PR without previous values: %s", got.Status)
	}
	h.run(t, 31*time.Minute)
	if got := h.get(t, 6); got.Status != model.VerificationNotDeployed {
		t.Errorf("change that never rolled out: %s", got.Status)
	}
}

func TestDeclinedRollback(t *testing.T) {
	h := newHarness(t)
	h.changes.rollbackPR = 0
	h.changes.merged = []gitops.MergedChange{cpuChange(7, "350m", "500m", t0)}
	h.cluster.deps["default/yolo-detector"] = deployment("350m", nil)
	h.run(t, time.Minute)
	h.metrics.signals = func(time.Time, time.Time) model.HealthSignals {
		return model.HealthSignals{Availability: 0.5, AvgReplicas: 1}
	}
	h.run(t, 31*time.Minute) // degraded at the end of the window
	h.run(t, time.Minute)    // rollback declined
	h.run(t, time.Minute)
	if got := h.get(t, 7); got.Status != RollbackDeclined {
		t.Fatalf("want RollbackDeclined, got %+v", got)
	}
	if len(h.changes.rollbacks) != 1 {
		t.Errorf("a declined rollback must not be retried, got %d calls", len(h.changes.rollbacks))
	}
}

func TestHalts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i, pr := range []int{3, 4} {
		if err := h.store.SaveVerification(ctx, &model.Verification{PR: pr, Target: "default/yolo-detector",
			MergedAt: t0.Add(time.Duration(i) * time.Hour), Status: model.VerificationRolledBack}); err != nil {
			t.Fatal(err)
		}
	}
	halted := h.run(t, 2*time.Hour)
	got, ok := halted["default/yolo-detector"]
	if !ok || len(got.FailedPRs) != 2 || !got.Since.Equal(t0.Add(time.Hour)) {
		t.Fatalf("want yolo-detector halted after #3 and #4, got %+v", halted)
	}
	if reason := HaltReason(got); !strings.Contains(reason, "#3, #4") || !strings.Contains(reason, ResumeAnnotation) {
		t.Errorf("unexpected reason %q", reason)
	}

	h.cluster.deps["default/yolo-detector"] = deployment("500m", map[string]string{
		ResumeAnnotation: t0.Add(90 * time.Minute).Format(time.RFC3339),
	})
	if halted := h.run(t, time.Minute); len(halted) != 0 {
		t.Errorf("the resume annotation must lift the halt, got %+v", halted)
	}
}

func TestCompareIgnoresMissingSignals(t *testing.T) {
	h := newHarness(t)
	nan := math.NaN()
	findings := h.v.compare(model.HealthSignals{ThrottleRatio: nan, AvgReplicas: 1},
		model.HealthSignals{ThrottleRatio: 0.9, AvgReplicas: nan, Availability: nan}, true)
	if len(findings) != 0 {
		t.Errorf("NaN signals must not produce findings, got %v", findings)
	}
}
