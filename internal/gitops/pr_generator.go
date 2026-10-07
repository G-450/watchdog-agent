package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"

	"gopkg.in/yaml.v3"
)

type ProposedState struct {
	CPURequests float64 `json:"cpu_requests,omitempty"`
	Replicas    *int32  `json:"replicas,omitempty"`
}

func OpenPR(ctx context.Context, cfg *config.Config, snapshotID string, recommendations []model.Recommendation) error {
	tmpDir, err := os.MkdirTemp("", "watchdog-infra-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is not set")
	}

	repoURL := fmt.Sprintf("https://%s@github.com/mithulpranav24/watchdog-infra.git", token)
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, tmpDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clone repo: %w", err)
	}

	branchName := fmt.Sprintf("watchdog/rec-%s", snapshotID)
	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "checkout", "-b", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	var patchedFiles []string
	for _, rec := range recommendations {
		parts := strings.Split(rec.Target, "/")
		if len(parts) != 2 {
			slog.Warn("Invalid target format", slog.String("target", rec.Target))
			continue
		}
		workloadName := parts[1]

		var state ProposedState
		if err := json.Unmarshal([]byte(rec.ProposedState), &state); err != nil {
			slog.Warn("Failed to parse ProposedState", slog.String("target", rec.Target), slog.Any("error", err))
			continue
		}

		deployPath := filepath.Join(tmpDir, "base", workloadName, "deployment.yaml")
		if _, err := os.Stat(deployPath); os.IsNotExist(err) {
			slog.Warn("Deployment YAML not found", slog.String("path", deployPath))
			continue
		}

		if err := patchDeploymentYaml(deployPath, state); err != nil {
			slog.Warn("Failed to patch YAML", slog.String("target", rec.Target), slog.Any("error", err))
			continue
		}

		patchedFiles = append(patchedFiles, deployPath)
	}

	if len(patchedFiles) == 0 {
		return fmt.Errorf("no files were patched")
	}

	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "watchdog@example.com")
	cmd.Run()
	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Watchdog Agent")
	cmd.Run()

	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}

	commitMsg := fmt.Sprintf("feat(gitops): apply recommendations from %s", snapshotID)
	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", commitMsg)
	if err := cmd.Run(); err != nil {
		slog.Warn("git commit failed, possibly no changes to commit", slog.Any("error", err))
		return nil
	}

	cmd = exec.CommandContext(ctx, "git", "-C", tmpDir, "push", "origin", branchName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push branch: %w", err)
	}

	prURL := "https://api.github.com/repos/mithulpranav24/watchdog-infra/pulls"
	prBody := map[string]interface{}{
		"title": fmt.Sprintf("Apply Watchdog Recommendations %s", snapshotID),
		"body":  "Automated PR from Watchdog Agent to apply AI recommendations.",
		"head":  branchName,
		"base":  "main",
	}
	prBodyBytes, _ := json.Marshal(prBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prURL, bytes.NewBuffer(prBodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create PR request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create PR, status code: %d", resp.StatusCode)
	}

	slog.Info("Successfully opened GitOps PR", slog.String("branch", branchName))
	return nil
}

func patchDeploymentYaml(path string, state ProposedState) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	if len(root.Content) == 0 {
		return fmt.Errorf("empty yaml")
	}

	doc := root.Content[0]
	spec := getMapValue(doc, "spec")
	if spec != nil {
		if state.Replicas != nil {
			setMapValue(spec, "replicas", fmt.Sprintf("%d", *state.Replicas))
		}
		if state.CPURequests != 0 {
			template := getMapValue(spec, "template")
			templateSpec := getMapValue(template, "spec")
			containers := getSequenceValue(templateSpec, "containers")
			if containers != nil && len(containers.Content) > 0 {
				container := containers.Content[0]
				resources := getOrCreateMapValue(container, "resources")
				requests := getOrCreateMapValue(resources, "requests")
				if requests != nil {
					setMapValue(requests, "cpu", fmt.Sprintf("%dm", int(state.CPURequests*1000)))
				}
			}
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func getMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func getSequenceValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			if node.Content[i+1].Kind == yaml.SequenceNode {
				return node.Content[i+1]
			}
			return nil
		}
	}
	return nil
}

func getOrCreateMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	// Create it
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content, keyNode, valNode)
	return valNode
}

func setMapValue(node *yaml.Node, key string, val string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = val
			if strings.HasSuffix(val, "m") {
				node.Content[i+1].Tag = "!!str"
			} else {
				node.Content[i+1].Tag = "!!int"
			}
			return
		}
	}
	// Create it if not found
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: val}
	if strings.HasSuffix(val, "m") {
		valNode.Tag = "!!str"
	} else {
		valNode.Tag = "!!int"
	}
	node.Content = append(node.Content, keyNode, valNode)
}
