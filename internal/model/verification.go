package model

import "time"

// Verification statuses, in lifecycle order.
const (
	VerificationWaiting     = "WaitingForRollout" // merged; the cluster does not run the new values yet
	VerificationVerifying   = "Verifying"         // running the new values; watching for regressions
	VerificationVerified    = "Verified"          // the window passed without a regression
	VerificationDegraded    = "Degraded"          // a regression was found; a rollback PR is due
	VerificationRollbackPR  = "RollbackProposed"  // a rollback PR is open for review
	VerificationRolledBack  = "RolledBack"        // the rollback PR was merged
	VerificationNotDeployed = "NotDeployed"       // the new values never rolled out in time
	VerificationSkipped     = "Skipped"           // cannot be verified, e.g. the PR has no previous values
)

// HealthSignals summarises a workload's health over a time window.
type HealthSignals struct {
	Restarts      float64 `json:"restarts"`       // container restarts in the window
	OOMKills      float64 `json:"oom_kills"`      // containers whose last termination was OOMKilled
	ThrottleRatio float64 `json:"throttle_ratio"` // throttled CFS periods / all periods, 0-1
	AvgReplicas   float64 `json:"avg_replicas"`   // average desired replicas; rises when an HPA scales out
	Availability  float64 `json:"availability"`   // average available / desired replicas, 0-1
}

// Verification tracks one merged Watchdog change from merge to verdict.
type Verification struct {
	PR         int            `json:"pr"`
	PRURL      string         `json:"pr_url"`
	Target     string         `json:"target"` // namespace/name
	MergedAt   time.Time      `json:"merged_at"`
	DeployedAt *time.Time     `json:"deployed_at,omitempty"`
	Changes    string         `json:"changes"`            // applied values, e.g. {"cpu":"350m"}
	Previous   string         `json:"previous,omitempty"` // values before the change
	Status     string         `json:"status"`
	Baseline   *HealthSignals `json:"baseline,omitempty"` // the window before the merge
	Observed   *HealthSignals `json:"observed,omitempty"` // the window after the rollout
	Findings   []string       `json:"findings,omitempty"`
	RollbackPR int            `json:"rollback_pr,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// HaltedWorkload is a workload Watchdog stopped changing after repeated rollbacks.
type HaltedWorkload struct {
	Target    string    `json:"target"`
	Since     time.Time `json:"since"`
	FailedPRs []int     `json:"failed_prs"`
}
