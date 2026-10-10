package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MergedChange is a Watchdog PR that a human merged into the base branch.
type MergedChange struct {
	PR       int
	URL      string
	Target   string // namespace/name
	MergedAt time.Time
	Changes  ChangeSet
	// Previous restores the manifest from before the change; nil when the PR did not record it.
	Previous *ChangeSet
	// RollbackOf is the PR whose change this PR restored; 0 for a recommendation.
	RollbackOf int
}

var (
	targetPattern     = regexp.MustCompile("\\*\\*Target:\\*\\* `([^`]+)`")
	previousPattern   = regexp.MustCompile(`<!-- watchdog:previous (\{.*?\}) -->`)
	rollbackOfPattern = regexp.MustCompile(`<!-- watchdog:rollback-of (\d+) -->`)
)

// MergedChanges lists Watchdog PRs merged since the given time, newest first. It reads the
// 100 most recently updated closed PRs, which covers any realistic verification window.
func (g *Generator) MergedChanges(ctx context.Context, since time.Time) ([]MergedChange, error) {
	ctx, cancel := context.WithTimeout(ctx, g.opTimeout)
	defer cancel()
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get GitHub token: %w", err)
	}
	g.token = token

	q := url.Values{}
	q.Set("state", "closed")
	q.Set("base", g.cfg.BaseBranch)
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	q.Set("per_page", "100")
	var prs []pullRequest
	if err := g.do(ctx, http.MethodGet, g.repoPath("pulls")+"?"+q.Encode(), nil, &prs); err != nil {
		return nil, err
	}

	var out []MergedChange
	for _, pr := range prs {
		if pr.MergedAt == nil || !strings.HasPrefix(pr.Head.Ref, g.cfg.BranchPrefix) {
			continue
		}
		mergedAt, err := time.Parse(time.RFC3339, *pr.MergedAt)
		if err != nil {
			return nil, fmt.Errorf("PR #%d: merged_at: %w", pr.Number, err)
		}
		if mergedAt.Before(since) {
			continue
		}
		mc, ok := parseMergedChange(pr, mergedAt)
		if !ok {
			slog.Warn("Ignoring merged PR without Watchdog markers", slog.Int("pr", pr.Number))
			continue
		}
		out = append(out, mc)
	}
	return out, nil
}

func parseMergedChange(pr pullRequest, mergedAt time.Time) (MergedChange, bool) {
	mc := MergedChange{PR: pr.Number, URL: pr.HTMLURL, MergedAt: mergedAt}
	m := targetPattern.FindStringSubmatch(pr.Body)
	if m == nil {
		return mc, false
	}
	mc.Target = m[1]
	if err := json.Unmarshal([]byte(prMarker(pr.Body)), &mc.Changes); err != nil || mc.Changes.IsEmpty() {
		return mc, false
	}
	if m := previousPattern.FindStringSubmatch(pr.Body); m != nil {
		var prev ChangeSet
		if err := json.Unmarshal([]byte(m[1]), &prev); err == nil && !prev.IsEmpty() {
			mc.Previous = &prev
		}
	}
	if m := rollbackOfPattern.FindStringSubmatch(pr.Body); m != nil {
		mc.RollbackOf, _ = strconv.Atoi(m[1])
	}
	return mc, true
}
