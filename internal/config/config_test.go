package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := []byte(`
agent:
  name: "test-agent"
  port: 8080
  log_level: "debug"
  reconcile_interval: "30s"

prometheus:
  url: "http://prom:9090"
  timeout: "5s"

opencost:
  url: "http://cost:9003"
  timeout: "5s"

kubernetes:
  namespace: "test-ns"
  excluded_namespaces:
    - kube-system

logging:
  format: "json"
  level: "info"
`)

	if err := os.WriteFile(configPath, yamlContent, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Agent.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Agent.Port)
	}
	if cfg.Prometheus.URL != "http://prom:9090" {
		t.Errorf("Expected prometheus url http://prom:9090, got %s", cfg.Prometheus.URL)
	}
	if cfg.Kubernetes.Namespace != "test-ns" {
		t.Errorf("Expected namespace test-ns, got %s", cfg.Kubernetes.Namespace)
	}
	if len(cfg.Kubernetes.ExcludedNamespaces) != 1 || cfg.Kubernetes.ExcludedNamespaces[0] != "kube-system" {
		t.Errorf("Expected excluded namespaces [kube-system], got %v", cfg.Kubernetes.ExcludedNamespaces)
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	localConfigPath := filepath.Join(tempDir, "config.local.yaml")

	yamlContent := []byte(`
agent:
  port: 8080
prometheus:
  url: "http://prom:9090"
opencost:
  url: "http://cost:9003"
`)
	if err := os.WriteFile(configPath, yamlContent, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	localYamlContent := []byte(`
agent:
  port: 9090
`)
	if err := os.WriteFile(localConfigPath, localYamlContent, 0644); err != nil {
		t.Fatalf("failed to write test local config file: %v", err)
	}

	os.Setenv("WATCHDOG_PROMETHEUS_URL", "http://env-prom:9090")
	defer os.Unsetenv("WATCHDOG_PROMETHEUS_URL")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Agent.Port != 9090 {
		t.Errorf("Expected overridden port 9090, got %d", cfg.Agent.Port)
	}
	if cfg.Prometheus.URL != "http://env-prom:9090" {
		t.Errorf("Expected env overridden prometheus url http://env-prom:9090, got %s", cfg.Prometheus.URL)
	}
}

func TestValidateConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := []byte(`
agent:
  name: "test-agent"
`)
	if err := os.WriteFile(configPath, yamlContent, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Expected error due to missing required fields, got nil")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	minimal := []byte("agent:\n  port: 8081\nprometheus:\n  url: \"http://prom:9090\"\nopencost:\n  url: \"http://cost:9003\"\npolicy:\n  min_confidence: 0.75\n")
	if err := os.WriteFile(configPath, minimal, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	tests := []struct {
		name      string
		got, want interface{}
	}{
		{"history window", cfg.Prometheus.HistoryWindow, "24h"},
		{"history step", cfg.Prometheus.HistoryStep, "15m"},
		{"cost window", cfg.OpenCost.Window, "1d"},
		{"min replicas", cfg.Policy.MinReplicas, 2},
		{"max step-down", cfg.Policy.MaxStepDownPercent, 0.30},
		{"configured confidence is kept", cfg.Policy.MinConfidence, 0.75},
		{"excluded namespaces", len(cfg.Policy.ExcludedNamespaces), 3},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestLoadConfig_RepositoryDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatalf("configs/config.yaml does not load: %v", err)
	}
	if cfg.Policy.MaxStepDownPercent != 0.30 || cfg.OpenCost.Window == "" {
		t.Errorf("unexpected repository defaults: %+v %+v", cfg.Policy, cfg.OpenCost)
	}
}
