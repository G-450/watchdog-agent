// Package verify checks merged Watchdog changes after they roll out and proposes a rollback
// PR when a change makes the workload worse. Workloads with repeated failed changes are halted.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/gitops"
	"watchdog-agent/internal/model"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResumeAnnotation on a Deployment, set to an RFC 3339 time, ignores failed changes merged
// before that time, which lifts a halt.
const ResumeAnnotation = "watchdog.finops.io/resume-after"

// RollbackDeclined means no rollback PR could be proposed: a human closed it without merging,
// or the manifest already holds the previous values.
const RollbackDeclined = "RollbackDeclined"

// Changes is the GitOps side: merged PRs in, rollback PRs out.
type Changes interface {
	MergedChanges(ctx context.Context, since time.Time) ([]gitops.MergedChange, error)
	OpenRollback(ctx context.Context, target string, restore gitops.ChangeSet, sourcePR int, findings []string) (int, error)
}

// Metrics measures workload health over a window.
type Metrics interface {
	HealthSignals(ctx context.Context, namespace, deployment string, start, end time.Time) (model.HealthSignals, error)
}

// Cluster reads live Deployments.
type Cluster interface {
	GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error)
}

// Store persists verification state.
type Store interface {
	SaveVerification(ctx context.Context, v *model.Verification) error
	GetVerification(ctx context.Context, pr int) (*model.Verification, error)
	GetVerifications(ctx context.Context, limit int) ([]*model.Verification, error)
	SetHaltedWorkloads(ctx context.Context, halted []model.HaltedWorkload) error
}

// Verifier runs one verification pass per reconciliation cycle.
type Verifier struct {
	cfg                              config.VerificationConfig
	window, rolloutTimeout, lookback time.Duration
	changes                          Changes
	metrics                          Metrics
	cluster                          Cluster
	store                            Store
	now                              func() time.Time
}

// New builds a Verifier; cfg must already be validated.
func New(cfg config.VerificationConfig, changes Changes, metrics Metrics, cluster Cluster, store Store) (*Verifier, error) {
	v := &Verifier{cfg: cfg, changes: changes, metrics: metrics, cluster: cluster, store: store, now: time.Now}
	for _, d := range []struct {
		name  string
		value string
		dst   *time.Duration
	}{{"window", cfg.Window, &v.window}, {"rollout_timeout", cfg.RolloutTimeout, &v.rolloutTimeout}, {"lookback", cfg.Lookback, &v.lookback}} {
		parsed, err := time.ParseDuration(d.value)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("verification.%s: invalid duration %q", d.name, d.value)
		}
		*d.dst = parsed
	}
	return v, nil
}

// Run records newly merged changes, advances every open verification, and returns the
// workloads that are halted, keyed by namespace/name.
func (v *Verifier) Run(ctx context.Context) map[string]model.HaltedWorkload {
	now := v.now()
	merged, err := v.changes.MergedChanges(ctx, now.Add(-v.lookback))
	if err != nil {
		slog.Warn("Failed to list merged Watchdog PRs; verifying known changes only", slog.Any("error", err))
	}
	// Oldest first, so a change and its rollback merged together are recorded in order.
	sort.Slice(merged, func(i, j int) bool { return merged[i].MergedAt.Before(merged[j].MergedAt) })
	for _, mc := range merged {
		if err := v.record(ctx, mc, now); err != nil {
			slog.Warn("Failed to record merged Watchdog PR", slog.Int("pr", mc.PR), slog.Any("error", err))
		}
	}

	records, err := v.store.GetVerifications(ctx, 500)
	if err != nil {
		slog.Warn("Failed to read verifications", slog.Any("error", err))
		return nil
	}
	for _, rec := range records {
		if err := v.advance(ctx, rec, now); err != nil {
			slog.Warn("Verification step failed", slog.Int("pr", rec.PR), slog.String("target", rec.Target), slog.Any("error", err))
		}
	}
	return v.halts(ctx, records)
}

// record stores a verification for a newly merged change, or marks the change a merged
// rollback restored.
func (v *Verifier) record(ctx context.Context, mc gitops.MergedChange, now time.Time) error {
	if mc.RollbackOf != 0 {
		rec, err := v.store.GetVerification(ctx, mc.RollbackOf)
		if err != nil || rec == nil || rec.Status == model.VerificationRolledBack {
			return err
		}
		rec.Status = model.VerificationRolledBack
		rec.RollbackPR = mc.PR
		rec.UpdatedAt = now
		slog.Info("Watchdog change rolled back", slog.Int("pr", rec.PR), slog.Int("rollback_pr", mc.PR), slog.String("target", rec.Target))
		return v.store.SaveVerification(ctx, rec)
	}
	if existing, err := v.store.GetVerification(ctx, mc.PR); err != nil || existing != nil {
		return err
	}
	changes, err := json.Marshal(mc.Changes)
	if err != nil {
		return fmt.Errorf("encode changes: %w", err)
	}
	rec := &model.Verification{
		PR: mc.PR, PRURL: mc.URL, Target: mc.Target, MergedAt: mc.MergedAt,
		Changes: string(changes), Status: model.VerificationWaiting, UpdatedAt: now,
	}
	if mc.Previous == nil {
		rec.Status = model.VerificationSkipped
		rec.Findings = []string{"The PR did not record the previous values, so the change cannot be verified or rolled back."}
	} else {
		prev, err := json.Marshal(mc.Previous)
		if err != nil {
			return fmt.Errorf("encode previous values: %w", err)
		}
		rec.Previous = string(prev)
	}
	slog.Info("Tracking merged Watchdog change", slog.Int("pr", rec.PR), slog.String("target", rec.Target), slog.String("status", rec.Status))
	return v.store.SaveVerification(ctx, rec)
}

// advance moves one verification through its lifecycle.
func (v *Verifier) advance(ctx context.Context, rec *model.Verification, now time.Time) error {
	ns, name, ok := strings.Cut(rec.Target, "/")
	if !ok {
		return fmt.Errorf("target %q is not namespace/name", rec.Target)
	}
	var prev gitops.ChangeSet
	if rec.Previous != "" {
		if err := json.Unmarshal([]byte(rec.Previous), &prev); err != nil {
			return fmt.Errorf("parse previous values: %w", err)
		}
	}
	log := slog.With(slog.Int("pr", rec.PR), slog.String("target", rec.Target))

	switch rec.Status {
	case model.VerificationWaiting:
		var applied gitops.ChangeSet
		if err := json.Unmarshal([]byte(rec.Changes), &applied); err != nil {
			return fmt.Errorf("parse changes: %w", err)
		}
		dep, err := v.cluster.GetDeployment(ctx, ns, name)
		if err != nil {
			return fmt.Errorf("get deployment: %w", err)
		}
		if runs(dep, applied, prev) && rolledOut(dep) {
			baseline, err := v.metrics.HealthSignals(ctx, ns, name, rec.MergedAt.Add(-v.window), rec.MergedAt)
			if err != nil {
				return fmt.Errorf("baseline: %w", err)
			}
			deployed := now
			rec.DeployedAt, rec.Baseline, rec.Status = &deployed, &baseline, model.VerificationVerifying
			log.Info("Watchdog change rolled out; verifying", slog.Duration("window", v.window))
		} else if now.Sub(rec.MergedAt) > v.rolloutTimeout {
			rec.Status = model.VerificationNotDeployed
			rec.Findings = []string{fmt.Sprintf("The cluster did not run the new values within %s of the merge.", v.rolloutTimeout)}
			log.Warn("Watchdog change did not roll out in time")
		} else {
			return nil
		}

	case model.VerificationVerifying:
		end := rec.DeployedAt.Add(v.window)
		final := !now.Before(end)
		if !final {
			end = now
		}
		observed, err := v.metrics.HealthSignals(ctx, ns, name, *rec.DeployedAt, end)
		if err != nil {
			return fmt.Errorf("observed: %w", err)
		}
		rec.Observed = &observed
		if findings := v.compare(*rec.Baseline, observed, final); len(findings) > 0 {
			rec.Status, rec.Findings = model.VerificationDegraded, findings
			log.Warn("Watchdog change degraded the workload", slog.Any("findings", findings))
		} else if final {
			rec.Status = model.VerificationVerified
			log.Info("Watchdog change verified")
		}

	case model.VerificationDegraded:
		pr, err := v.changes.OpenRollback(ctx, rec.Target, prev, rec.PR, rec.Findings)
		if err != nil {
			return fmt.Errorf("open rollback PR: %w", err)
		}
		if pr == 0 {
			rec.Status = RollbackDeclined
			rec.Findings = append(rec.Findings, "No rollback PR was opened: it was declined, or the manifest already has the previous values.")
		} else {
			rec.Status, rec.RollbackPR = model.VerificationRollbackPR, pr
		}

	default:
		return nil // terminal, or waiting for a human to merge the rollback
	}
	rec.UpdatedAt = now
	return v.store.SaveVerification(ctx, rec)
}

// compare returns the regressions in observed relative to baseline. Restarts and OOM kills
// are checked throughout the window; the averaged signals only once it has passed.
func (v *Verifier) compare(baseline, observed model.HealthSignals, final bool) []string {
	var findings []string
	if observed.Restarts > baseline.Restarts+v.cfg.MaxNewRestarts {
		findings = append(findings, fmt.Sprintf("%.0f container restarts after the change, %.0f in the %s before it.",
			observed.Restarts, baseline.Restarts, v.window))
	}
	if observed.OOMKills > baseline.OOMKills {
		findings = append(findings, fmt.Sprintf("OOMKilled containers after the change: %.0f.", observed.OOMKills))
	}
	if !final {
		return findings
	}
	if known(observed.ThrottleRatio, baseline.ThrottleRatio) && observed.ThrottleRatio-baseline.ThrottleRatio > v.cfg.MaxThrottleIncrease {
		findings = append(findings, fmt.Sprintf("CPU throttling rose from %.0f%% to %.0f%%.",
			baseline.ThrottleRatio*100, observed.ThrottleRatio*100))
	}
	if known(observed.AvgReplicas, baseline.AvgReplicas) && observed.AvgReplicas-baseline.AvgReplicas > v.cfg.MaxReplicaIncrease {
		findings = append(findings, fmt.Sprintf("Average replicas rose from %.1f to %.1f, so the change costs more than it saves.",
			baseline.AvgReplicas, observed.AvgReplicas))
	}
	if known(observed.Availability) && observed.Availability < v.cfg.MinAvailability {
		findings = append(findings, fmt.Sprintf("Availability averaged %.0f%%, below the %.0f%% minimum.",
			observed.Availability*100, v.cfg.MinAvailability*100))
	}
	return findings
}

func known(values ...float64) bool {
	for _, x := range values {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// failed reports whether a verification counts towards halting its workload.
func failed(status string) bool {
	switch status {
	case model.VerificationDegraded, model.VerificationRollbackPR, model.VerificationRolledBack, RollbackDeclined:
		return true
	}
	return false
}

// halts finds workloads with at least MaxRollbacks failed changes since their resume time,
// persists the set and returns it.
func (v *Verifier) halts(ctx context.Context, records []*model.Verification) map[string]model.HaltedWorkload {
	byTarget := map[string][]*model.Verification{}
	for _, rec := range records {
		if failed(rec.Status) {
			byTarget[rec.Target] = append(byTarget[rec.Target], rec)
		}
	}
	halted := map[string]model.HaltedWorkload{}
	list := make([]model.HaltedWorkload, 0)
	for target, recs := range byTarget {
		if len(recs) < v.cfg.MaxRollbacks {
			continue
		}
		resume := v.resumeAfter(ctx, target)
		h := model.HaltedWorkload{Target: target}
		for _, rec := range recs {
			if rec.MergedAt.After(resume) {
				h.FailedPRs = append(h.FailedPRs, rec.PR)
				if rec.MergedAt.After(h.Since) {
					h.Since = rec.MergedAt
				}
			}
		}
		if len(h.FailedPRs) >= v.cfg.MaxRollbacks {
			sort.Ints(h.FailedPRs)
			halted[target] = h
			list = append(list, h)
		}
	}
	if err := v.store.SetHaltedWorkloads(ctx, list); err != nil {
		slog.Warn("Failed to save halted workloads", slog.Any("error", err))
	}
	return halted
}

// resumeAfter reads the workload's resume annotation; the zero time when unset or unreadable.
func (v *Verifier) resumeAfter(ctx context.Context, target string) time.Time {
	ns, name, _ := strings.Cut(target, "/")
	dep, err := v.cluster.GetDeployment(ctx, ns, name)
	if err != nil {
		return time.Time{}
	}
	value := dep.Annotations[ResumeAnnotation]
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		slog.Warn("Ignoring unparseable resume annotation", slog.String("target", target), slog.String("value", value))
		return time.Time{}
	}
	return t
}

// HaltReason explains a halt in a recommendation's rejection reason.
func HaltReason(h model.HaltedWorkload) string {
	prs := make([]string, len(h.FailedPRs))
	for i, pr := range h.FailedPRs {
		prs[i] = fmt.Sprintf("#%d", pr)
	}
	return fmt.Sprintf("halted after %d failed changes (%s); set annotation %s to an RFC 3339 time after %s to resume",
		len(h.FailedPRs), strings.Join(prs, ", "), ResumeAnnotation, h.Since.UTC().Format(time.RFC3339))
}

// runs reports whether the Deployment's first container runs the applied values. Only fields
// the PR actually changed (those with a recorded previous value) are compared.
func runs(dep *appsv1.Deployment, applied, prev gitops.ChangeSet) bool {
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		return false
	}
	requests := containers[0].Resources.Requests
	if prev.CPU != "" && !sameQuantity(applied.CPU, requests.Cpu()) {
		return false
	}
	if prev.Memory != "" && !sameQuantity(applied.Memory, requests.Memory()) {
		return false
	}
	if prev.Replicas != nil && applied.Replicas != nil && (dep.Spec.Replicas == nil || *dep.Spec.Replicas != *applied.Replicas) {
		return false
	}
	return true
}

func sameQuantity(want string, have *resource.Quantity) bool {
	q, err := resource.ParseQuantity(want)
	return err == nil && have != nil && q.Cmp(*have) == 0
}

// rolledOut reports whether the Deployment's latest spec is fully rolled out and available.
func rolledOut(dep *appsv1.Deployment) bool {
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	s := dep.Status
	return s.ObservedGeneration >= dep.Generation && s.UpdatedReplicas == desired &&
		s.Replicas == desired && s.AvailableReplicas == desired
}
