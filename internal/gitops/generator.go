// Package gitops turns approved recommendations into pull requests on the infra repository,
// where ArgoCD applies them after a human merges.
package gitops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

// Generator turns approved recommendations into pull requests on the infra repo.
// It keeps one branch and one PR per workload and only touches the lines whose values change.
type Generator struct {
	cfg         config.GitOpsConfig
	excluded    map[string]bool
	token       string
	owner, repo string
	cloneURL    string        // tests point it at a local bare repository
	http        *http.Client  // per-request timeout from cfg.Timeout
	opTimeout   time.Duration // deadline for all work on one workload
}

// New validates cfg and builds a Generator. token is the GitHub token; it is required.
func New(cfg config.GitOpsConfig, excluded []string, token string) (*Generator, error) {
	if token == "" {
		return nil, errors.New("gitops: GitHub token is empty")
	}
	owner, repo, ok := strings.Cut(cfg.Repo, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return nil, fmt.Errorf("gitops: repo must be owner/name, got %q", cfg.Repo)
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("gitops: timeout: %w", err)
	}
	opTimeout, err := time.ParseDuration(cfg.OperationTimeout)
	if err != nil {
		return nil, fmt.Errorf("gitops: operation_timeout: %w", err)
	}
	if opTimeout <= 0 {
		return nil, fmt.Errorf("gitops: operation_timeout must be positive, got %s", opTimeout)
	}
	ex := make(map[string]bool, len(excluded))
	for _, ns := range excluded {
		ex[ns] = true
	}
	return &Generator{
		cfg:       cfg,
		excluded:  ex,
		token:     token,
		owner:     owner,
		repo:      repo,
		cloneURL:  "https://github.com/" + owner + "/" + repo + ".git",
		http:      &http.Client{Timeout: timeout},
		opTimeout: opTimeout,
	}, nil
}

// workloadPlan is the merged change for one workload across the cycle's approved recommendations.
type workloadPlan struct {
	namespace, name string
	changes         ChangeSet
	recs            []model.Recommendation
	recChanges      []ChangeSet // per rec, parallel to recs
}

// Apply opens or updates one PR per workload for the approved recommendations in recs.
// Each workload is handled independently; failures are logged and returned joined.
func (g *Generator) Apply(ctx context.Context, recs []model.Recommendation) error {
	var errs []error
	for _, p := range g.plan(recs) {
		if err := g.applyWorkloadWithDeadline(ctx, p); err != nil {
			err = fmt.Errorf("%s/%s: %w", p.namespace, p.name, err)
			slog.Warn("GitOps PR generation failed", slog.String("target", p.namespace+"/"+p.name), slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// dnsLabel matches Kubernetes namespace and Deployment names, which also keeps paths and branch names safe.
var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// plan filters recs to actionable approved changes and merges them per workload, in input order.
func (g *Generator) plan(recs []model.Recommendation) []*workloadPlan {
	var plans []*workloadPlan
	byTarget := map[string]*workloadPlan{}
	for _, rec := range recs {
		if rec.Status != "Approved" {
			continue
		}
		log := slog.With(slog.String("target", rec.Target), slog.String("action", rec.Action))
		ns, name, ok := strings.Cut(rec.Target, "/")
		if !ok || !dnsLabel.MatchString(ns) || !dnsLabel.MatchString(name) {
			log.Warn("Skipping GitOps change: target is not namespace/name")
			continue
		}
		if g.excluded[ns] {
			log.Warn("Skipping GitOps change: namespace is excluded")
			continue
		}
		cs, err := diffStates(rec.CurrentState, rec.ProposedState)
		if err != nil {
			log.Warn("Skipping GitOps change: unreadable state", slog.Any("error", err))
			continue
		}
		if cs.IsEmpty() {
			log.Debug("Skipping GitOps change: proposed state equals current state")
			continue
		}

		p := byTarget[rec.Target]
		if p == nil {
			p = &workloadPlan{namespace: ns, name: name}
			byTarget[rec.Target] = p
			plans = append(plans, p)
		}
		if conflicts := p.changes.merge(cs); len(conflicts) > 0 {
			log.Warn("Conflicting recommendations for the same workload; keeping the first",
				slog.Any("fields", conflicts))
		}
		p.recs = append(p.recs, rec)
		p.recChanges = append(p.recChanges, cs)
	}
	return plans
}

// applyWorkloadWithDeadline bounds one workload's work so a stalled git clone or push
// (git has no network timeout of its own) cannot block the reconciliation loop.
func (g *Generator) applyWorkloadWithDeadline(ctx context.Context, p *workloadPlan) error {
	ctx, cancel := context.WithTimeout(ctx, g.opTimeout)
	defer cancel()
	return g.applyWorkload(ctx, p)
}

// applyWorkload brings the workload's PR in line with p.changes.
func (g *Generator) applyWorkload(ctx context.Context, p *workloadPlan) error {
	target := p.namespace + "/" + p.name
	log := slog.With(slog.String("target", target))
	branch := g.cfg.BranchPrefix + p.namespace + "-" + p.name
	marker, err := p.changes.marker()
	if err != nil {
		return err
	}

	pr, declined, err := g.branchPRs(ctx, branch)
	if err != nil {
		return fmt.Errorf("look up PRs: %w", err)
	}
	if pr != nil && prMarker(pr.Body) == marker {
		log.Debug("GitOps PR already proposes this change", slog.Int("pr", pr.Number))
		return nil
	}
	if pr == nil {
		// A human closed this exact proposal without merging: treat it as a "no".
		for _, d := range declined {
			if prMarker(d.Body) == marker {
				log.Debug("GitOps change was declined in a closed PR", slog.Int("pr", d.Number))
				return nil
			}
		}
	}

	dir := path.Join(g.cfg.ManifestRoot, p.namespace, p.name)
	exists, err := g.pathExists(ctx, dir)
	if err != nil {
		return fmt.Errorf("check %s: %w", dir, err)
	}
	if !exists {
		log.Warn("Skipping GitOps change: no manifest folder in the infra repo", slog.String("path", dir))
		return nil
	}

	tmp, err := os.MkdirTemp("", "watchdog-gitops-*")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmp); err != nil {
			log.Warn("Failed to remove GitOps work dir", slog.String("dir", tmp), slog.Any("error", err))
		}
	}()

	repoDir := filepath.Join(tmp, "repo")
	if _, err := g.runGit(ctx, tmp, "clone", "--quiet", "--depth", "1", "--single-branch", "--no-tags",
		"--branch", g.cfg.BaseBranch, g.cloneURL, repoDir); err != nil {
		return err
	}
	if _, err := g.runGit(ctx, repoDir, "checkout", "--quiet", "-b", branch); err != nil {
		return err
	}

	files, err := readManifests(repoDir, dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		log.Warn("Skipping GitOps change: manifest folder has no YAML files", slog.String("path", dir))
		return nil
	}
	res, err := patchWorkload(files, p.name, p.changes)
	if err != nil {
		return err
	}
	for _, reason := range res.Skipped {
		log.Warn("GitOps field not applied", slog.String("reason", reason))
	}
	if len(res.Edits) == 0 {
		log.Info("GitOps change not needed: manifest already matches or no field applies", slog.String("path", res.Path))
		return nil
	}

	if err := os.WriteFile(filepath.Join(repoDir, filepath.FromSlash(res.Path)), res.Data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", res.Path, err)
	}
	if _, err := g.runGit(ctx, repoDir, "add", "--", res.Path); err != nil {
		return err
	}
	if _, err := g.runGit(ctx, repoDir,
		"-c", "user.name="+g.cfg.AuthorName, "-c", "user.email="+g.cfg.AuthorEmail,
		"commit", "--quiet", "--no-verify", "-m", commitMessage(p, res.Edits)); err != nil {
		return err
	}
	// The branch belongs to the agent; force-push replaces any earlier proposal.
	if _, err := g.runGit(ctx, repoDir, "push", "--quiet", "--force", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return err
	}

	title := prTitle(p)
	body := prBody(p, res, marker)
	if pr == nil {
		created, err := g.createPR(ctx, branch, title, body)
		if err != nil {
			return fmt.Errorf("open PR: %w", err)
		}
		log.Info("Opened GitOps PR", slog.Int("pr", created.Number), slog.String("url", created.HTMLURL))
		return nil
	}
	if err := g.updatePR(ctx, pr.Number, title, body); err != nil {
		return fmt.Errorf("update PR #%d: %w", pr.Number, err)
	}
	log.Info("Updated GitOps PR", slog.Int("pr", pr.Number), slog.String("url", pr.HTMLURL))
	return nil
}

// readManifests returns every .yaml/.yml file directly inside dir (repo-relative, slash-separated).
func readManifests(repoDir, dir string) ([]manifestFile, error) {
	entries, err := os.ReadDir(filepath.Join(repoDir, filepath.FromSlash(dir)))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var files []manifestFile
	for _, e := range entries {
		ext := strings.ToLower(path.Ext(e.Name()))
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		rel := path.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		files = append(files, manifestFile{Path: rel, Data: data})
	}
	return files, nil
}

var markerPattern = regexp.MustCompile(`<!-- watchdog:changes (\{.*?\}) -->`)

// prMarker extracts the change-set marker from a PR body, or "" if absent.
func prMarker(body string) string {
	m := markerPattern.FindAllStringSubmatch(body, -1)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1][1]
}

func actions(p *workloadPlan) string {
	seen := map[string]bool{}
	var out []string
	for _, r := range p.recs {
		a := r.Action
		if a == "" {
			a = "RIGHTSIZE"
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return strings.Join(out, " + ")
}

func valueOrUnset(v string) string {
	if v == "" {
		return "unset"
	}
	return v
}

func commitMessage(p *workloadPlan, edits []fieldEdit) string {
	parts := make([]string, len(edits))
	for i, e := range edits {
		parts[i] = fmt.Sprintf("%s %s→%s", e.Field, valueOrUnset(e.Old), e.New)
	}
	return fmt.Sprintf("chore(%s/%s): %s %s", p.namespace, p.name, actions(p), strings.Join(parts, ", "))
}

func prTitle(p *workloadPlan) string {
	return fmt.Sprintf("watchdog: %s for %s/%s", actions(p), p.namespace, p.name)
}

var fieldLabels = map[string]string{"cpu": "CPU request", "memory": "Memory request", "replicas": "Replicas"}

func prBody(p *workloadPlan, res patchResult, marker string) string {
	applied := map[string]bool{}
	for _, e := range res.Edits {
		applied[e.Field] = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Watchdog recommendation\n\n")
	fmt.Fprintf(&b, "**Target:** `%s/%s` (Deployment in `%s`)\n\n", p.namespace, p.name, res.Path)
	b.WriteString("| Field | Current | Proposed |\n|---|---|---|\n")
	for _, e := range res.Edits {
		fmt.Fprintf(&b, "| %s | `%s` | `%s` |\n", fieldLabels[e.Field], valueOrUnset(e.Old), e.New)
	}
	if len(res.Skipped) > 0 {
		b.WriteString("\n**Not applied:**\n")
		for _, s := range res.Skipped {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}

	for i, rec := range p.recs {
		cs := p.recChanges[i]
		contributes := (cs.CPU != "" && applied["cpu"]) || (cs.Memory != "" && applied["memory"]) ||
			(cs.Replicas != nil && applied["replicas"])
		fmt.Fprintf(&b, "\n### %s\n\n", rec.Action)
		if !contributes {
			b.WriteString("_None of this recommendation's fields were applied; see above._\n\n")
		}
		fmt.Fprintf(&b, "- Expected monthly savings: $%.2f\n", rec.ExpectedSavings)
		fmt.Fprintf(&b, "- Confidence: %.2f\n", rec.ConfidenceScore)
		if rec.SupportingEvidence != "" {
			fmt.Fprintf(&b, "- Evidence: %s\n", rec.SupportingEvidence)
		}
		if len(rec.RuleTrace) > 0 {
			fmt.Fprintf(&b, "- Rule trace: `%s`\n", strings.Join(rec.RuleTrace, "` → `"))
		}
	}

	b.WriteString("\n---\nOpened by the Watchdog agent. It needs human review; ArgoCD applies it only after merge.\n")
	fmt.Fprintf(&b, "\n<!-- watchdog:changes %s -->\n", marker)
	return b.String()
}
