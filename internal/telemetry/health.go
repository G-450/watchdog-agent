package telemetry

import (
	"context"
	"fmt"
	"time"

	wmodel "watchdog-agent/internal/model"
)

// HealthSignals measures a Deployment's health over [start, end] for post-deployment verification.
// Signals with no data (no HPA, no CPU limit, no restarts) read as zero.
func (c *Client) HealthSignals(ctx context.Context, namespace, deployment string, start, end time.Time) (wmodel.HealthSignals, error) {
	var s wmodel.HealthSignals
	queries := healthQueries(namespace, deployment, end.Sub(start))
	targets := []*float64{&s.Restarts, &s.OOMKills, &s.ThrottleRatio, &s.AvgReplicas, &s.Availability}
	for i, q := range queries {
		v, err := c.executeQueryAt(ctx, q, end)
		if err != nil {
			return wmodel.HealthSignals{}, fmt.Errorf("health query %d for %s/%s: %w", i, namespace, deployment, err)
		}
		*targets[i] = v
	}
	return s, nil
}

// healthQueries returns, in order: restarts, OOM-killed containers, CPU throttle ratio,
// average desired replicas and average availability over the window ending at query time.
func healthQueries(namespace, deployment string, window time.Duration) []string {
	r := promRange(window)
	pods := fmt.Sprintf(`namespace=%q, pod=~%q`, namespace, DeploymentPodPattern(deployment))
	dep := fmt.Sprintf(`namespace=%q, deployment=%q`, namespace, deployment)
	return []string{
		fmt.Sprintf(`sum(increase(kube_pod_container_status_restarts_total{%s}[%s]))`, pods, r),
		fmt.Sprintf(`count(max_over_time(kube_pod_container_status_last_terminated_reason{%s, reason="OOMKilled"}[%s]) == 1)`, pods, r),
		fmt.Sprintf(`sum(increase(container_cpu_cfs_throttled_periods_total{%s, container!=""}[%s])) / sum(increase(container_cpu_cfs_periods_total{%s, container!=""}[%s]))`, pods, r, pods, r),
		fmt.Sprintf(`avg_over_time(sum(kube_deployment_spec_replicas{%s})[%s:1m])`, dep, r),
		fmt.Sprintf(`avg_over_time((sum(kube_deployment_status_replicas_available{%s}) / sum(kube_deployment_spec_replicas{%s}))[%s:1m])`, dep, dep, r),
	}
}

// promRange renders d as a PromQL range of whole seconds, at least one minute.
func promRange(d time.Duration) string {
	if d < time.Minute {
		d = time.Minute
	}
	return fmt.Sprintf("%ds", int64(d.Seconds()))
}
