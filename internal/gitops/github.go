package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxErrorBody = 4096

// pullRequest is the subset of GitHub's pull request object the generator reads.
type pullRequest struct {
	Number   int     `json:"number"`
	State    string  `json:"state"` // "open" or "closed"
	Body     string  `json:"body"`
	HTMLURL  string  `json:"html_url"`
	MergedAt *string `json:"merged_at"`
	Head     struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// githubError is a non-2xx response from the GitHub API.
type githubError struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *githubError) Error() string {
	return fmt.Sprintf("github %s %s: %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// do sends a GitHub REST API request. A nil out discards the response body; path is
// relative to the API root and must already be escaped.
func (g *Generator) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(g.cfg.APIURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, g.scrubErr(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if err != nil {
			return fmt.Errorf("github %s %s: %d (body unreadable: %w)", method, path, resp.StatusCode, err)
		}
		return &githubError{Method: method, Path: path, Status: resp.StatusCode, Body: g.scrub(strings.TrimSpace(string(msg)))}
	}
	if out == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("github %s %s: read body: %w", method, path, err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github %s %s: decode: %w", method, path, err)
	}
	return nil
}

// branchPRs returns the open PR whose head is branch (or nil), the closed unmerged ones, and
// when the most recent one was merged (zero if none). One request covers all three, so the
// common "nothing to do" cycle stays a single API call.
func (g *Generator) branchPRs(ctx context.Context, branch string) (open *pullRequest, declined []pullRequest, lastMerged time.Time, err error) {
	q := url.Values{}
	q.Set("head", g.owner+":"+branch)
	q.Set("state", "all")
	q.Set("per_page", "100")
	var prs []pullRequest
	if err := g.do(ctx, http.MethodGet, g.repoPath("pulls")+"?"+q.Encode(), nil, &prs); err != nil {
		return nil, nil, time.Time{}, err
	}
	for i := range prs {
		switch {
		case prs[i].State == "open" && open == nil:
			open = &prs[i]
		case prs[i].State == "closed" && prs[i].MergedAt == nil:
			declined = append(declined, prs[i])
		case prs[i].MergedAt != nil:
			t, err := time.Parse(time.RFC3339, *prs[i].MergedAt)
			if err != nil {
				return nil, nil, time.Time{}, fmt.Errorf("PR #%d: merged_at: %w", prs[i].Number, err)
			}
			if t.After(lastMerged) {
				lastMerged = t
			}
		}
	}
	return open, declined, lastMerged, nil
}

// pathExists reports whether path exists on the base branch.
func (g *Generator) pathExists(ctx context.Context, path string) (bool, error) {
	q := url.Values{}
	q.Set("ref", g.cfg.BaseBranch)
	err := g.do(ctx, http.MethodGet, g.repoPath("contents", path)+"?"+q.Encode(), nil, nil)
	var ghErr *githubError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ghErr) && ghErr.Status == http.StatusNotFound:
		return false, nil
	default:
		return false, err
	}
}

func (g *Generator) createPR(ctx context.Context, branch, title, body string) (*pullRequest, error) {
	in := map[string]string{"head": branch, "base": g.cfg.BaseBranch, "title": title, "body": body}
	var pr pullRequest
	if err := g.do(ctx, http.MethodPost, g.repoPath("pulls"), in, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (g *Generator) updatePR(ctx context.Context, number int, title, body string) error {
	in := map[string]string{"title": title, "body": body}
	return g.do(ctx, http.MethodPatch, g.repoPath("pulls", fmt.Sprint(number)), in, nil)
}

// repoPath builds /repos/{owner}/{repo}/<segments>, escaping each path segment.
func (g *Generator) repoPath(segments ...string) string {
	var b strings.Builder
	b.WriteString("/repos/" + url.PathEscape(g.owner) + "/" + url.PathEscape(g.repo))
	for _, s := range segments {
		for _, part := range strings.Split(s, "/") {
			b.WriteString("/" + url.PathEscape(part))
		}
	}
	return b.String()
}
