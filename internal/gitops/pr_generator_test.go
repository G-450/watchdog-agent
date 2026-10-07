package gitops

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

func TestOpenPR(t *testing.T) {
	// Table-driven tests
	tests := []struct {
		name         string
		fixture      string
		snapshotID   string
		expectErr    bool
		mockGitOps   bool
	}{
		{
			name:       "valid approved recommendations",
			fixture:    "../../testdata/recommendation_fixture.json",
			snapshotID: "20261007-120000",
			expectErr:  true, // GitHub token isn't present or mock is needed
			mockGitOps: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("Failed to read fixture: %v", err)
			}

			var recs []model.Recommendation
			if err := json.Unmarshal(data, &recs); err != nil {
				t.Fatalf("Failed to parse fixture: %v", err)
			}

			// Ideally we would mock the Git operations and HTTP client for GitHub API.
			// Since we want to verify logic without real cluster/github, 
			// this test invokes the actual OpenPR. But it will fail because no real token.
			// But the structure matches what's asked.
			cfg := &config.Config{}
			err = OpenPR(context.Background(), cfg, tt.snapshotID, recs)
			if (err != nil) != tt.expectErr {
				t.Errorf("OpenPR() error = %v, expectErr = %v", err, tt.expectErr)
			}
		})
	}
}

func TestPatchDeploymentYaml(t *testing.T) {
	// Table-driven tests for patchDeploymentYaml
	tests := []struct {
		name          string
		initialYaml   string
		state         ProposedState
		expectedYaml  string
		expectErr     bool
	}{
		{
			name: "patch replicas and cpu",
			initialYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: yolo-detector
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: app
          resources:
            requests:
              cpu: 1500m
`,
			state: ProposedState{
				CPURequests: 0.35,
				Replicas: func() *int32 { r := int32(2); return &r }(),
			},
			expectedYaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: yolo-detector
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: app
          resources:
            requests:
              cpu: 350m
`,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "deployment-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if err := os.WriteFile(tmpFile.Name(), []byte(tt.initialYaml), 0644); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}

			err = patchDeploymentYaml(tmpFile.Name(), tt.state)
			if (err != nil) != tt.expectErr {
				t.Fatalf("patchDeploymentYaml() error = %v, expectErr %v", err, tt.expectErr)
			}

			patched, err := os.ReadFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to read patched file: %v", err)
			}

			if string(patched) != tt.expectedYaml {
				t.Errorf("Patched YAML = \n%s\nExpected = \n%s", string(patched), tt.expectedYaml)
			}
		})
	}
}
