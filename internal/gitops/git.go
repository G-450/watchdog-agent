package gitops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// gitEnv returns the environment for git subprocesses. The token travels as an HTTP header
// set through GIT_CONFIG_* (as actions/checkout does), never in the URL or argv.
func (g *Generator) gitEnv() []string {
	pairs := [][2]string{
		{"credential.helper", ""}, // no credential manager prompts or cached credentials
		{"core.autocrlf", "false"},
	}
	if u, err := url.Parse(g.cloneURL); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.token))
		pairs = append(pairs, [2]string{
			"http." + u.Scheme + "://" + u.Host + "/.extraheader",
			"AUTHORIZATION: basic " + basic,
		})
	}

	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never",
		fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(pairs)))
	for i, p := range pairs {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, p[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, p[1]))
	}
	return env
}

// runGit runs git in dir and returns stdout. Errors carry stderr with the token removed.
func (g *Generator) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// git spawns helpers (git-remote-https) that can keep the output pipes open after git
	// itself is killed on cancellation; stop waiting for them shortly after.
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = g.gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctxErr)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], g.scrubErr(err), g.scrub(strings.TrimSpace(stderr.String())))
	}
	return stdout.String(), nil
}

// scrub removes the token, raw or base64-encoded, from s.
func (g *Generator) scrub(s string) string {
	if g.token == "" {
		return s
	}
	s = strings.ReplaceAll(s, g.token, "[REDACTED]")
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.token))
	return strings.ReplaceAll(s, basic, "[REDACTED]")
}

func (g *Generator) scrubErr(err error) error {
	if err == nil {
		return nil
	}
	if msg := err.Error(); g.scrub(msg) != msg {
		return errors.New(g.scrub(msg))
	}
	return err
}
