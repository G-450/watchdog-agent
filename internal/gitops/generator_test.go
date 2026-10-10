package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

const fakeToken = "ghp_FAKEtoken0123456789abcdefghijklmnopq"

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// git runs a git command for test setup or inspection and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.autocrlf=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func branchExists(bare, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = bare
	return cmd.Run() == nil
}

// seedRemote creates a bare repository whose main branch holds testdata/manifests with LF endings.
func seedRemote(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "seed")
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	git(t, root, "init", "--quiet", "--initial-branch=main", work)

	src := filepath.Join("..", "..", "testdata", "manifests")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(strings.ReplaceAll(string(data), "\r\n", "\n")), 0o644)
	})
	if err != nil {
		t.Fatalf("copy manifests: %v", err)
	}
	git(t, work, "add", ".")
	git(t, work, "-c", "user.name=seed", "-c", "user.email=seed@example.com", "commit", "--quiet", "-m", "seed")
	git(t, work, "push", "--quiet", bare, "HEAD:refs/heads/main")
	return bare
}

// fakeGitHub serves the subset of the GitHub REST API the generator uses and records every call.
type fakeGitHub struct {
	t    *testing.T
	bare string

	mu       sync.Mutex
	calls    []string // "METHOD /path"
	bodies   map[string]string
	prs      map[string][]pullRequest // by head branch, any state
	failPOST int                      // status code for POST /pulls, 0 = succeed
	failBody string
}

func (f *fakeGitHub) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if got := r.Header.Get("Authorization"); got != "Bearer "+fakeToken {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPatch) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.t.Errorf("decode request body: %v", err)
		}
	}

	const repo = "/repos/o/r/"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == repo+"pulls":
		if want := "o:"; !strings.HasPrefix(r.URL.Query().Get("head"), want) || r.URL.Query().Get("state") != "all" {
			f.t.Errorf("unexpected PR query %q", r.URL.RawQuery)
		}
		branch := strings.TrimPrefix(r.URL.Query().Get("head"), "o:")
		list := append([]pullRequest{}, f.prs[branch]...)
		writeJSON(f.t, w, http.StatusOK, list)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, repo+"contents/"):
		p := strings.TrimPrefix(r.URL.Path, repo+"contents/")
		cmd := exec.Command("git", "cat-file", "-e", r.URL.Query().Get("ref")+":"+p)
		cmd.Dir = f.bare
		if cmd.Run() != nil {
			writeJSON(f.t, w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		writeJSON(f.t, w, http.StatusOK, []any{})
	case r.Method == http.MethodPost && r.URL.Path == repo+"pulls":
		f.bodies["POST"] = req.Body
		if f.failPOST != 0 {
			w.WriteHeader(f.failPOST)
			if _, err := io.WriteString(w, f.failBody); err != nil {
				f.t.Errorf("write: %v", err)
			}
			return
		}
		writeJSON(f.t, w, http.StatusCreated, pullRequest{Number: 7, HTMLURL: "https://example/pr/7"})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, repo+"pulls/"):
		f.bodies["PATCH"] = req.Body
		writeJSON(f.t, w, http.StatusOK, map[string]any{})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func loadFixtureRecs(t *testing.T) []model.Recommendation {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "recommendation_fixture.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var recs []model.Recommendation
	if err := json.Unmarshal(data, &recs); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return recs
}

func approved(target, action, current, proposed string) model.Recommendation {
	return model.Recommendation{
		Target: target, Action: action, CurrentState: current, ProposedState: proposed,
		Status: "Approved", ExpectedSavings: 1.5, ConfidenceScore: 0.8,
	}
}

// testEnv is one isolated run: a seeded remote, a fake API and a generator wired to both.
type testEnv struct {
	bare string
	api  *fakeGitHub
	gen  *Generator
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	bare := seedRemote(t)
	api := &fakeGitHub{t: t, bare: bare, bodies: map[string]string{}, prs: map[string][]pullRequest{}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	cfg := config.GitOpsConfig{
		Enabled: true, Repo: "o/r", BaseBranch: "main", ManifestRoot: "workloads",
		BranchPrefix: "watchdog/", APIURL: srv.URL, Timeout: "10s", OperationTimeout: "1m",
		AuthorName: "Watchdog Agent", AuthorEmail: "watchdog-agent@users.noreply.github.com",
	}
	gen, err := New(cfg, []string{"kube-system", "monitoring", "watchdog"}, StaticToken(fakeToken))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gen.cloneURL = bare
	return &testEnv{bare: bare, api: api, gen: gen}
}

// withMergedPR records a Watchdog PR on branch merged `ago` before a fixed clock, with a 24h cooldown.
func withMergedPR(branch string, ago time.Duration) func(e *testEnv) {
	return func(e *testEnv) {
		now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
		merged := now.Add(-ago).Format(time.RFC3339)
		e.gen.now = func() time.Time { return now }
		e.gen.cooldown = 24 * time.Hour
		e.api.prs[branch] = []pullRequest{{Number: 4, State: "closed", MergedAt: &merged, Body: "<!-- watchdog:changes {\"cpu\":\"700m\"} -->"}}
	}
}

func (e *testEnv) show(t *testing.T, branch, path string) string {
	t.Helper()
	return git(t, e.bare, "show", branch+":"+path)
}

// diffLines returns the added and removed lines between main and branch.
func (e *testEnv) diffLines(t *testing.T, branch string) (added, removed []string) {
	t.Helper()
	out := git(t, e.bare, "diff", "--unified=0", "main", branch)
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			added = append(added, l[1:])
		case strings.HasPrefix(l, "-"):
			removed = append(removed, l[1:])
		}
	}
	return added, removed
}

func TestApply(t *testing.T) {
	requireGit(t)
	fixture := loadFixtureRecs(t)
	yoloRec, opencostRec := fixture[0], fixture[1]
	const (
		yoloBranch = "watchdog/default-yolo-detector"
		calcBranch = "watchdog/default-flask-calc"
		yoloPath   = "workloads/default/yolo-detector/yolo.yaml"
		calcPath   = "workloads/default/flask-calc/calc-deployment.yaml"
	)

	tests := []struct {
		name    string
		recs    []model.Recommendation
		setup   func(e *testEnv)
		wantErr string
		check   func(t *testing.T, e *testEnv)
	}{
		{
			name: "real yolo-detector rec",
			recs: []model.Recommendation{yoloRec},
			check: func(t *testing.T, e *testEnv) {
				added, removed := e.diffLines(t, yoloBranch)
				if len(added) != 1 || len(removed) != 1 {
					t.Fatalf("want a one-line diff, got +%q -%q", added, removed)
				}
				if added[0] != `            cpu: "350m"      # The HPA will base its 70% target off this value` {
					t.Errorf("unexpected line %q", added[0])
				}
				got := e.show(t, yoloBranch, yoloPath)
				for _, s := range []string{"kind: Service", "kind: HorizontalPodAutoscaler", "averageUtilization: 70"} {
					if !strings.Contains(got, s) {
						t.Errorf("patched file lost %q", s)
					}
				}
				if strings.Contains(got, "replicas:") {
					t.Error("replicas must not be added to an HPA-managed Deployment")
				}
				if n := e.api.count("POST"); n != 1 {
					t.Errorf("POST count = %d, want 1", n)
				}
				body := e.api.bodies["POST"]
				for _, s := range []string{`<!-- watchdog:changes {"cpu":"350m"} -->`, "RIGHTSIZE_CPU_DOWN", "$3.60", "0.74", "human review"} {
					if !strings.Contains(body, s) {
						t.Errorf("PR body missing %q", s)
					}
				}
				msg := git(t, e.bare, "log", "-1", "--format=%an <%ae>%n%s", yoloBranch)
				if !strings.Contains(msg, "Watchdog Agent <watchdog-agent@users.noreply.github.com>") ||
					!strings.Contains(msg, "chore(default/yolo-detector): RIGHTSIZE_CPU_DOWN cpu 500m→350m") {
					t.Errorf("unexpected commit %q", msg)
				}
			},
		},
		{
			name: "real rejected opencost rec",
			recs: []model.Recommendation{opencostRec},
			check: func(t *testing.T, e *testEnv) {
				if n := e.api.count(""); n != 0 {
					t.Errorf("want no API calls, got %v", e.api.calls)
				}
			},
		},
		{
			name: "flask-calc cpu change creates resources",
			recs: []model.Recommendation{approved("default/flask-calc", "SCALE_UP_CPU_SAFETY",
				`{"cpu_requests": 0, "replicas": 2}`, `{"cpu_requests": 0.25, "replicas": 2}`)},
			check: func(t *testing.T, e *testEnv) {
				added, removed := e.diffLines(t, calcBranch)
				want := []string{"        resources:", "          requests:", `            cpu: "250m"`}
				if strings.Join(added, "\n") != strings.Join(want, "\n") || len(removed) != 0 {
					t.Errorf("diff +%q -%q", added, removed)
				}
				got := e.show(t, calcBranch, calcPath)
				if !strings.Contains(got, "        - containerPort: 4591\n        resources:\n") ||
					!strings.Contains(got, "kind: Service") || !strings.Contains(got, "nodePort: 30001") {
					t.Errorf("unexpected file:\n%s", got)
				}
			},
		},
		{
			name: "sidecar-demo cpu change is skipped",
			recs: []model.Recommendation{approved("default/sidecar-demo", "RIGHTSIZE_CPU_DOWN",
				`{"cpu_requests": 0.6}`, `{"cpu_requests": 0.45}`)},
			check: func(t *testing.T, e *testEnv) {
				if branchExists(e.bare, "watchdog/default-sidecar-demo") || e.api.count("POST") != 0 {
					t.Error("multi-container workload must not be pushed")
				}
			},
		},
		{
			name: "open PR with identical marker",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				e.api.prs[yoloBranch] = []pullRequest{{Number: 3, State: "open", Body: "text\n<!-- watchdog:changes {\"cpu\":\"350m\"} -->\n"}}
			},
			check: func(t *testing.T, e *testEnv) {
				if n := e.api.count(""); n != 1 || e.api.count("GET /repos/o/r/pulls") != 1 {
					t.Errorf("want only the PR lookup, got %v", e.api.calls)
				}
				if branchExists(e.bare, yoloBranch) {
					t.Error("branch must not be pushed")
				}
			},
		},
		{
			name: "open PR with different marker",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				e.api.prs[yoloBranch] = []pullRequest{
					{Number: 3, State: "open", Body: "<!-- watchdog:changes {\"cpu\":\"400m\"} -->"},
					// An older declined proposal of the same change must not block updating the open PR.
					{Number: 2, State: "closed", Body: "<!-- watchdog:changes {\"cpu\":\"350m\"} -->"},
				}
			},
			check: func(t *testing.T, e *testEnv) {
				if !branchExists(e.bare, yoloBranch) {
					t.Error("branch not pushed")
				}
				if e.api.count("PATCH /repos/o/r/pulls/3") != 1 || e.api.count("POST") != 0 {
					t.Errorf("want one PATCH and no POST, got %v", e.api.calls)
				}
				if !strings.Contains(e.api.bodies["PATCH"], `{"cpu":"350m"}`) {
					t.Error("PATCH body lacks the new marker")
				}
			},
		},
		{
			name: "closed unmerged PR with identical marker",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				e.api.prs[yoloBranch] = []pullRequest{{Number: 4, State: "closed", Body: "<!-- watchdog:changes {\"cpu\":\"350m\"} -->"}}
			},
			check: func(t *testing.T, e *testEnv) {
				if n := e.api.count(""); n != 1 || e.api.count("GET /repos/o/r/pulls") != 1 {
					t.Errorf("want only the PR lookup, got %v", e.api.calls)
				}
				if branchExists(e.bare, yoloBranch) {
					t.Error("a declined change must not be pushed again")
				}
			},
		},
		{
			name: "closed unmerged PR with different marker",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				e.api.prs[yoloBranch] = []pullRequest{{Number: 4, State: "closed", Body: "<!-- watchdog:changes {\"cpu\":\"400m\"} -->"}}
			},
			check: func(t *testing.T, e *testEnv) {
				if !branchExists(e.bare, yoloBranch) {
					t.Error("branch not pushed")
				}
				if e.api.count("POST /repos/o/r/pulls") != 1 || e.api.count("PATCH") != 0 {
					t.Errorf("want a new PR, got %v", e.api.calls)
				}
			},
		},
		{
			name: "merged PR with identical marker is not a decline",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				merged := "2026-10-01T00:00:00Z"
				e.api.prs[yoloBranch] = []pullRequest{{Number: 4, State: "closed", MergedAt: &merged, Body: "<!-- watchdog:changes {\"cpu\":\"350m\"} -->"}}
			},
			check: func(t *testing.T, e *testEnv) {
				// The fake main still holds 500m (as if the merge was reverted), so a new PR is due.
				if e.api.count("POST /repos/o/r/pulls") != 1 {
					t.Errorf("want a new PR, got %v", e.api.calls)
				}
			},
		},
		{
			name:  "merged PR inside cooldown defers the next change",
			recs:  []model.Recommendation{yoloRec},
			setup: withMergedPR(yoloBranch, 23*time.Hour),
			check: func(t *testing.T, e *testEnv) {
				if n := e.api.count(""); n != 1 || e.api.count("GET /repos/o/r/pulls") != 1 {
					t.Errorf("want only the PR lookup, got %v", e.api.calls)
				}
				if branchExists(e.bare, yoloBranch) {
					t.Error("branch must not be pushed during cooldown")
				}
			},
		},
		{
			name:  "merged PR past cooldown allows the next change",
			recs:  []model.Recommendation{yoloRec},
			setup: withMergedPR(yoloBranch, 25*time.Hour),
			check: func(t *testing.T, e *testEnv) {
				if e.api.count("POST /repos/o/r/pulls") != 1 {
					t.Errorf("want a new PR, got %v", e.api.calls)
				}
			},
		},
		{
			name: "cooldown does not block updating an open PR",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				withMergedPR(yoloBranch, time.Hour)(e)
				e.api.prs[yoloBranch] = append(e.api.prs[yoloBranch],
					pullRequest{Number: 5, State: "open", Body: "<!-- watchdog:changes {\"cpu\":\"400m\"} -->"})
			},
			check: func(t *testing.T, e *testEnv) {
				if e.api.count("PATCH /repos/o/r/pulls/5") != 1 {
					t.Errorf("want the open PR updated, got %v", e.api.calls)
				}
			},
		},
		{
			name: "GitHub 422 on create",
			recs: []model.Recommendation{yoloRec},
			setup: func(e *testEnv) {
				e.api.failPOST = http.StatusUnprocessableEntity
				// A misbehaving server echoing the credential must not leak it into errors.
				e.api.failBody = `{"message":"Validation Failed","errors":[{"message":"A pull request already exists"}],"echo":"` + fakeToken + `"}`
			},
			wantErr: "A pull request already exists",
		},
		{
			name: "target folder missing",
			recs: []model.Recommendation{approved("default/ghost", "RIGHTSIZE_CPU_DOWN",
				`{"cpu_requests": 0.5}`, `{"cpu_requests": 0.35}`)},
			check: func(t *testing.T, e *testEnv) {
				if branchExists(e.bare, "watchdog/default-ghost") || e.api.count("POST") != 0 {
					t.Error("missing folder must not produce a push or PR")
				}
			},
		},
		{
			name: "excluded namespace marked Approved",
			recs: []model.Recommendation{approved("monitoring/prometheus", "RIGHTSIZE_CPU_DOWN",
				`{"cpu_requests": 0.5}`, `{"cpu_requests": 0.35}`)},
			check: func(t *testing.T, e *testEnv) {
				if n := e.api.count(""); n != 0 {
					t.Errorf("want no API calls, got %v", e.api.calls)
				}
			},
		},
		{
			name: "replicas change on HPA-managed Deployment",
			recs: []model.Recommendation{approved("default/yolo-detector", "RIGHTSIZE_REPLICAS",
				`{"replicas": 3, "cpu_requests": 0.5}`, `{"replicas": 2, "cpu_requests": 0.5}`)},
			check: func(t *testing.T, e *testEnv) {
				if branchExists(e.bare, yoloBranch) || e.api.count("POST") != 0 {
					t.Error("replicas must be skipped when an HPA manages the Deployment")
				}
			},
		},
		{
			name: "cpu rounding",
			recs: []model.Recommendation{approved("default/flask-calc", "SCALE_UP_CPU_SAFETY",
				`{"cpu_requests": 0.9}`, `{"cpu_requests": 1.005}`)},
			check: func(t *testing.T, e *testEnv) {
				if !strings.Contains(e.show(t, calcBranch, calcPath), `cpu: "1005m"`) {
					t.Error("want 1005m")
				}
			},
		},
		{
			name: "two recs for one workload share one PR",
			recs: []model.Recommendation{
				approved("default/flask-calc", "RIGHTSIZE_CPU_DOWN", `{"cpu_requests": 0.5, "replicas": 2}`, `{"cpu_requests": 0.35, "replicas": 2}`),
				approved("default/flask-calc", "RIGHTSIZE_REPLICAS", `{"replicas": 2, "cpu_requests": 0.5}`, `{"replicas": 1, "cpu_requests": 0.5}`),
			},
			check: func(t *testing.T, e *testEnv) {
				got := e.show(t, calcBranch, calcPath)
				if !strings.Contains(got, "  replicas: 1\n") || !strings.Contains(got, `cpu: "350m"`) {
					t.Errorf("want both changes, got:\n%s", got)
				}
				if n := e.api.count("POST"); n != 1 {
					t.Errorf("POST count = %d, want 1", n)
				}
			},
		},
		{
			name:    "git failure does not leak the token",
			recs:    []model.Recommendation{yoloRec},
			setup:   func(e *testEnv) { e.gen.cloneURL = "https://127.0.0.1:1/o/r.git" },
			wantErr: "git clone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(e)
			}
			err := e.gen.Apply(context.Background(), tt.recs)
			if err != nil && strings.Contains(err.Error(), fakeToken) {
				t.Fatalf("error leaks the token: %v", err)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Apply: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Apply error = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, e)
			}
		})
	}
}

func TestApply_SecondCycleIsIdempotent(t *testing.T) {
	requireGit(t)
	e := newTestEnv(t)
	recs := loadFixtureRecs(t)[:1]
	if err := e.gen.Apply(context.Background(), recs); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// What GitHub would now report for the branch.
	e.api.prs["watchdog/default-yolo-detector"] = []pullRequest{{Number: 7, State: "open", Body: e.api.bodies["POST"]}}
	head := git(t, e.bare, "rev-parse", "watchdog/default-yolo-detector")
	if err := e.gen.Apply(context.Background(), recs); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if e.api.count("POST") != 1 || e.api.count("PATCH") != 0 {
		t.Errorf("second cycle wrote to GitHub: %v", e.api.calls)
	}
	if git(t, e.bare, "rev-parse", "watchdog/default-yolo-detector") != head {
		t.Error("second cycle pushed")
	}
}

func TestApply_HungRemoteHitsDeadline(t *testing.T) {
	requireGit(t)
	e := newTestEnv(t)

	// A git remote that accepts connections and never answers.
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(hung.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close does not wait on the handler

	e.gen.cloneURL = hung.URL + "/o/r.git"
	e.gen.opTimeout = 2 * time.Second

	start := time.Now()
	err := e.gen.Apply(context.Background(), loadFixtureRecs(t)[:1])
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("want a deadline error, got %v", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatal("error leaks the token")
	}
	// opTimeout plus exec's WaitDelay is the worst case; much longer means the deadline did not stop git.
	if elapsed > 15*time.Second {
		t.Errorf("Apply took %s; the deadline did not stop git", elapsed)
	}
	if e.api.count("POST") != 0 {
		t.Error("no PR may be opened after a failed clone")
	}
}

func TestNew(t *testing.T) {
	base := config.GitOpsConfig{Repo: "o/r", Timeout: "30s", OperationTimeout: "2m"}
	tests := []struct {
		name    string
		mutate  func(c *config.GitOpsConfig)
		token   string
		wantErr bool
	}{
		{name: "valid", token: fakeToken},
		{name: "empty token", wantErr: true},
		{name: "bad repo", token: fakeToken, mutate: func(c *config.GitOpsConfig) { c.Repo = "repo" }, wantErr: true},
		{name: "bad timeout", token: fakeToken, mutate: func(c *config.GitOpsConfig) { c.Timeout = "x" }, wantErr: true},
		{name: "bad operation timeout", token: fakeToken, mutate: func(c *config.GitOpsConfig) { c.OperationTimeout = "x" }, wantErr: true},
		{name: "zero operation timeout", token: fakeToken, mutate: func(c *config.GitOpsConfig) { c.OperationTimeout = "0s" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}
			g, err := New(cfg, nil, StaticToken(tt.token))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), fakeToken) {
				t.Error("error leaks the token")
			}
			if g != nil && g.cloneURL != "https://github.com/o/r.git" {
				t.Errorf("cloneURL = %q", g.cloneURL)
			}
		})
	}
}

func TestPRMarker(t *testing.T) {
	tests := []struct{ body, want string }{
		{"", ""},
		{"no marker", ""},
		{"x <!-- watchdog:changes {\"cpu\":\"350m\"} --> y", `{"cpu":"350m"}`},
		{"<!-- watchdog:changes {\"cpu\":\"1m\"} -->\r\n<!-- watchdog:changes {\"cpu\":\"2m\"} -->", `{"cpu":"2m"}`},
	}
	for _, tt := range tests {
		if got := prMarker(tt.body); got != tt.want {
			t.Errorf("prMarker(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func ExampleChangeSet_marker() {
	r := int32(2)
	m, err := ChangeSet{CPU: "350m", Memory: "256Mi", Replicas: &r}.marker()
	fmt.Println(m, err)
	// Output: {"cpu":"350m","memory":"256Mi","replicas":2} <nil>
}
