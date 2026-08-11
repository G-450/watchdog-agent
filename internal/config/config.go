package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
}

// OpenCostConfig holds the configuration for OpenCost connectivity.
type OpenCostConfig struct {
	URL     string `yaml:"url"`
	Timeout string `yaml:"timeout"`
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

// Config represents the full configuration tree.
type Config struct {
	Agent      AgentConfig      `yaml:"agent"`
	Prometheus PrometheusConfig `yaml:"prometheus"`
	OpenCost   OpenCostConfig   `yaml:"opencost"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
	Logging    LoggingConfig    `yaml:"logging"`
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
	return nil
}
