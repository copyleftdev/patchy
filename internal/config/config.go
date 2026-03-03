package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/patchy-mcp/patchy/internal/observability"
	"github.com/patchy-mcp/patchy/internal/policy"
)

// Config is the top-level PATCHY configuration.
type Config struct {
	Server  ServerConfig              `yaml:"server"`
	Binary  BinaryConfig              `yaml:"binary"`
	Policy  policy.PolicyConfig       `yaml:"policy"`
	Runner  RunnerConfig              `yaml:"runner"`
	Store   StoreConfig               `yaml:"store"`
	Logging observability.LogConfig   `yaml:"logging"`
}

// ServerConfig holds MCP server settings.
type ServerConfig struct {
	Name      string `yaml:"name"`
	Version   string `yaml:"version"`
	Transport string `yaml:"transport"`
	Listen    string `yaml:"listen"`
}

// BinaryConfig holds binary discovery settings.
type BinaryConfig struct {
	SearchPath string            `yaml:"search_path"`
	PdtmPath   string            `yaml:"pdtm_path"`
	Overrides  map[string]string `yaml:"overrides"`
}

// RunnerConfig holds runner execution settings.
type RunnerConfig struct {
	MaxStdout      string `yaml:"max_stdout"`
	MaxStderr      string `yaml:"max_stderr"`
	DefaultTimeout string `yaml:"default_timeout"`
	BaseOutputDir  string `yaml:"base_output_dir"`
}

// StoreConfig holds artifact store settings.
type StoreConfig struct {
	BaseDir   string          `yaml:"base_dir"`
	Retention RetentionConfig `yaml:"retention"`
}

// RetentionConfig defines artifact retention durations.
type RetentionConfig struct {
	Runs      string `yaml:"runs"`
	Pipelines string `yaml:"pipelines"`
	Updates   string `yaml:"updates"`
}

// Defaults returns a Config with sensible defaults.
func Defaults() Config {
	home := os.Getenv("HOME")
	baseDir := filepath.Join(home, ".patchy")

	return Config{
		Server: ServerConfig{
			Name:      "patchy",
			Version:   "0.1.0",
			Transport: "stdio",
		},
		Binary: BinaryConfig{
			SearchPath: filepath.Join(home, ".pdtm", "go", "bin"),
		},
		Policy: policy.PolicyConfig{
			RateLimits: policy.RateLimitConfig{
				Defaults: policy.RateLimitEntry{RequestsPerMin: 30, Burst: 5},
			},
			Concurrency: policy.ConcurrencyConfig{
				Defaults: policy.ConcurrencyEntry{MaxConcurrent: 3},
			},
			Timeouts: policy.TimeoutConfig{
				Defaults: policy.TimeoutEntry{Default: "5m", Max: "30m"},
			},
		},
		Runner: RunnerConfig{
			MaxStdout:      "10MB",
			MaxStderr:      "1MB",
			DefaultTimeout: "5m",
			BaseOutputDir:  baseDir,
		},
		Store: StoreConfig{
			BaseDir: baseDir,
			Retention: RetentionConfig{
				Runs:      "7d",
				Pipelines: "30d",
				Updates:   "90d",
			},
		},
		Logging: observability.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		},
	}
}

// Load reads config from file, applying defaults for missing values.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		path = findConfigFile()
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	// Apply env overrides (always, even without a config file)
	applyEnvOverrides(&cfg)

	return cfg, nil
}

// findConfigFile searches standard locations.
func findConfigFile() string {
	candidates := []string{
		"patchy.yaml",
		"patchy.yml",
		filepath.Join(os.Getenv("HOME"), ".config", "patchy", "patchy.yaml"),
		filepath.Join(os.Getenv("HOME"), ".patchy", "patchy.yaml"),
		"/etc/patchy/patchy.yaml",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PATCHY_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("PATCHY_LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("PATCHY_TRANSPORT"); v != "" {
		cfg.Server.Transport = v
	}
	if v := os.Getenv("PATCHY_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("PATCHY_BASE_DIR"); v != "" {
		cfg.Store.BaseDir = v
		cfg.Runner.BaseOutputDir = v
	}
	if v := os.Getenv("PATCHY_BINARY_PATH"); v != "" {
		cfg.Binary.SearchPath = v
	}
	if v := os.Getenv("PATCHY_SCOPE"); v != "" {
		for _, entry := range strings.Split(v, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if strings.Contains(entry, "/") {
				if _, _, err := net.ParseCIDR(entry); err == nil {
					cfg.Policy.Scope.AllowCIDRs = append(cfg.Policy.Scope.AllowCIDRs, entry)
					continue
				}
			}
			cfg.Policy.Scope.AllowDomains = append(cfg.Policy.Scope.AllowDomains, entry)
		}
	}
}

// ParseDuration parses a duration string with fallback.
func ParseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// ParseSize parses a human-readable size like "10MB" into bytes.
func ParseSize(s string, fallback int64) int64 {
	if s == "" {
		return fallback
	}
	var n int64
	var unit string
	if _, err := fmt.Sscanf(s, "%d%s", &n, &unit); err == nil {
		switch unit {
		case "KB", "kb":
			return n * 1024
		case "MB", "mb":
			return n * 1024 * 1024
		case "GB", "gb":
			return n * 1024 * 1024 * 1024
		}
		return n
	}
	return fallback
}
