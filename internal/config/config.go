package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds the configuration for the agent itself.
type AgentConfig struct {
	Name              string `yaml:"name"`
	Port              int    `yaml:"port"`
	LogLevel          string `yaml:"log_level"`
	ReconcileInterval string `yaml:"reconcile_interval"`
}

// PrometheusConfig holds the configuration for Prometheus connectivity.
type PrometheusConfig struct {
	URL     string `yaml:"url"`
	Timeout string `yaml:"timeout"`
	// HistoryWindow and HistoryStep control the usage series sent to the forecaster.
	HistoryWindow string `yaml:"history_window"`
	HistoryStep   string `yaml:"history_step"`
}

// OpenCostConfig holds the configuration for OpenCost connectivity.
type OpenCostConfig struct {
	URL     string `yaml:"url"`
	Timeout string `yaml:"timeout"`
	// Window is the allocation window used to derive monthly run-rate costs.
	Window string `yaml:"window"`
}

// KubernetesConfig holds the configuration for Kubernetes discovery.
type KubernetesConfig struct {
	Namespace          string   `yaml:"namespace"`
	ExcludedNamespaces []string `yaml:"excluded_namespaces"`
}

// LoggingConfig holds the configuration for structured logging.
type LoggingConfig struct {
	Format string `yaml:"format"`
	Level  string `yaml:"level"`
}

// StorageConfig holds the configuration for data persistence.
type StorageConfig struct {
	Path string `yaml:"path"`
}

// AIServiceConfig holds the configuration for the Python AI Service.
type AIServiceConfig struct {
	URL string `yaml:"url"`
}

// PolicyConfig holds the guardrails applied to AI recommendations.
type PolicyConfig struct {
	MinReplicas        int      `yaml:"min_replicas"`
	MaxStepDownPercent float64  `yaml:"max_step_down_percent"`
	MinConfidence      float64  `yaml:"min_confidence"`
	MinCPURequest      float64  `yaml:"min_cpu_request"`
	ExcludedNamespaces []string `yaml:"excluded_namespaces"`
}

// APIConfig holds dashboard API configuration.
type APIConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// GitOpsConfig controls the GitOps PR generator.
type GitOpsConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Repo         string `yaml:"repo"` // "owner/name"
	BaseBranch   string `yaml:"base_branch"`
	ManifestRoot string `yaml:"manifest_root"`
	BranchPrefix string `yaml:"branch_prefix"`
	APIURL       string `yaml:"api_url"`
	Timeout      string `yaml:"timeout"` // per GitHub API request
	// OperationTimeout bounds all work for one workload, including git clone and push.
	OperationTimeout string `yaml:"operation_timeout"`
	AuthorName       string `yaml:"author_name"`
	AuthorEmail      string `yaml:"author_email"`
	// Cooldown is how long after a merged Watchdog PR the agent waits before proposing
	// another change to the same workload, so new telemetry reflects the change. "0" disables it.
	Cooldown string `yaml:"cooldown"`
	// Auth is "token" (GITHUB_TOKEN from the environment) or "app" (a GitHub App installation).
	Auth           string `yaml:"auth"`
	AppID          int64  `yaml:"app_id"`
	InstallationID int64  `yaml:"installation_id"`
	PrivateKeyPath string `yaml:"private_key_path"` // the App's PEM key, mounted from a Secret
}

type Config struct {
	Agent      AgentConfig      `yaml:"agent"`
	Prometheus PrometheusConfig `yaml:"prometheus"`
	OpenCost   OpenCostConfig   `yaml:"opencost"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
	Logging    LoggingConfig    `yaml:"logging"`
	Storage    StorageConfig    `yaml:"storage"`
	AIService  AIServiceConfig  `yaml:"ai_service"`
	Policy     PolicyConfig     `yaml:"policy"`
	API        APIConfig        `yaml:"api"`
	GitOps     GitOpsConfig     `yaml:"gitops"`
}

// Load reads the configuration from the given path.
// It also checks for an optional config.local.yaml in the same directory for overrides.
// Environment variables will override any values specified in the YAML files.
func Load(path string) (*Config, error) {
	config := &Config{}

	// Load base config
	if err := loadYAML(path, config); err != nil {
		return nil, fmt.Errorf("failed to load base config: %w", err)
	}

	// Try loading local config override (ignore if it doesn't exist)
	localPath := strings.TrimSuffix(path, ".yaml") + ".local.yaml"
	if _, err := os.Stat(localPath); err == nil {
		if err := loadYAML(localPath, config); err != nil {
			return nil, fmt.Errorf("failed to load local config override: %w", err)
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(config)

	applyDefaults(config)

	// Validate configuration
	if err := validate(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// loadYAML reads and unmarshals a YAML file into the config struct.
func loadYAML(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, config)
}

// applyEnvOverrides allows overriding config values with environment variables.
func applyEnvOverrides(config *Config) {
	if val := os.Getenv("WATCHDOG_AGENT_NAME"); val != "" {
		config.Agent.Name = val
	}
	if val := os.Getenv("WATCHDOG_AGENT_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Agent.Port = port
		}
	}
	if val := os.Getenv("WATCHDOG_AGENT_LOG_LEVEL"); val != "" {
		config.Agent.LogLevel = val
	}
	if val := os.Getenv("WATCHDOG_AGENT_RECONCILE_INTERVAL"); val != "" {
		config.Agent.ReconcileInterval = val
	}
	if val := os.Getenv("WATCHDOG_PROMETHEUS_URL"); val != "" {
		config.Prometheus.URL = val
	}
	if val := os.Getenv("WATCHDOG_PROMETHEUS_TIMEOUT"); val != "" {
		config.Prometheus.Timeout = val
	}
	if val := os.Getenv("WATCHDOG_OPENCOST_URL"); val != "" {
		config.OpenCost.URL = val
	}
	if val := os.Getenv("WATCHDOG_OPENCOST_TIMEOUT"); val != "" {
		config.OpenCost.Timeout = val
	}
	if val := os.Getenv("WATCHDOG_KUBERNETES_NAMESPACE"); val != "" {
		config.Kubernetes.Namespace = val
	}
	if val := os.Getenv("WATCHDOG_LOGGING_FORMAT"); val != "" {
		config.Logging.Format = val
	}
	if val := os.Getenv("WATCHDOG_LOGGING_LEVEL"); val != "" {
		config.Logging.Level = val
	}
	if val := os.Getenv("WATCHDOG_STORAGE_PATH"); val != "" {
		config.Storage.Path = val
	}
	if val := os.Getenv("WATCHDOG_AI_SERVICE_URL"); val != "" {
		config.AIService.URL = val
	}
	if val := os.Getenv("WATCHDOG_API_ALLOWED_ORIGINS"); val != "" {
		config.API.AllowedOrigins = strings.Split(val, ",")
	}
	if val := os.Getenv("WATCHDOG_GITOPS_ENABLED"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			config.GitOps.Enabled = enabled
		} else {
			slog.Warn("Ignoring WATCHDOG_GITOPS_ENABLED: not a boolean", slog.String("value", val))
		}
	}
	if val := os.Getenv("WATCHDOG_GITOPS_REPO"); val != "" {
		config.GitOps.Repo = val
	}
}

// applyDefaults fills optional settings that were omitted from every config source.
func applyDefaults(config *Config) {
	if config.Prometheus.HistoryWindow == "" {
		config.Prometheus.HistoryWindow = "24h"
	}
	if config.Prometheus.HistoryStep == "" {
		config.Prometheus.HistoryStep = "15m"
	}
	if config.OpenCost.Window == "" {
		config.OpenCost.Window = "1d"
	}
	if config.Policy.MinReplicas == 0 {
		config.Policy.MinReplicas = 2
	}
	if config.Policy.MaxStepDownPercent == 0 {
		config.Policy.MaxStepDownPercent = 0.30
	}
	if config.Policy.MinConfidence == 0 {
		config.Policy.MinConfidence = 0.60
	}
	if config.Policy.MinCPURequest == 0 {
		config.Policy.MinCPURequest = 0.05
	}
	if config.Policy.ExcludedNamespaces == nil {
		config.Policy.ExcludedNamespaces = []string{"kube-system", "monitoring", "watchdog"}
	}
	if config.GitOps.BaseBranch == "" {
		config.GitOps.BaseBranch = "main"
	}
	if config.GitOps.ManifestRoot == "" {
		config.GitOps.ManifestRoot = "workloads"
	}
	if config.GitOps.BranchPrefix == "" {
		config.GitOps.BranchPrefix = "watchdog/"
	}
	if config.GitOps.APIURL == "" {
		config.GitOps.APIURL = "https://api.github.com"
	}
	if config.GitOps.Timeout == "" {
		config.GitOps.Timeout = "30s"
	}
	if config.GitOps.OperationTimeout == "" {
		config.GitOps.OperationTimeout = "2m"
	}
	if config.GitOps.AuthorName == "" {
		config.GitOps.AuthorName = "Watchdog Agent"
	}
	if config.GitOps.AuthorEmail == "" {
		config.GitOps.AuthorEmail = "watchdog-agent@users.noreply.github.com"
	}
	if config.GitOps.Cooldown == "" {
		config.GitOps.Cooldown = "24h"
	}
	if config.GitOps.Auth == "" {
		config.GitOps.Auth = "token"
	}
	if config.GitOps.PrivateKeyPath == "" {
		config.GitOps.PrivateKeyPath = "/etc/watchdog/github-app/private-key.pem"
	}
}

// validate ensures required fields are set.
func validate(config *Config) error {
	if config.Agent.Port == 0 {
		return fmt.Errorf("agent.port is required")
	}
	if config.Prometheus.URL == "" {
		return fmt.Errorf("prometheus.url is required")
	}
	if config.OpenCost.URL == "" {
		return fmt.Errorf("opencost.url is required")
	}
	if config.GitOps.Enabled {
		owner, name, ok := strings.Cut(config.GitOps.Repo, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("gitops.repo must be in owner/name form, got %q", config.GitOps.Repo)
		}
		if _, err := time.ParseDuration(config.GitOps.Timeout); err != nil {
			return fmt.Errorf("gitops.timeout: %w", err)
		}
		if _, err := time.ParseDuration(config.GitOps.OperationTimeout); err != nil {
			return fmt.Errorf("gitops.operation_timeout: %w", err)
		}
		if d, err := time.ParseDuration(config.GitOps.Cooldown); err != nil {
			return fmt.Errorf("gitops.cooldown: %w", err)
		} else if d < 0 {
			return fmt.Errorf("gitops.cooldown must not be negative, got %s", d)
		}
		switch config.GitOps.Auth {
		case "token":
		case "app":
			if config.GitOps.AppID <= 0 || config.GitOps.InstallationID <= 0 {
				return fmt.Errorf("gitops.auth is app but app_id or installation_id is not set")
			}
		default:
			return fmt.Errorf("gitops.auth must be token or app, got %q", config.GitOps.Auth)
		}
	}
	return nil
}
