package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type ApplicationLogConfig struct {
	Name    string   `yaml:"name"`
	Service string   `yaml:"service"`
	Paths   []string `yaml:"paths"`
}

type EngineConfig struct {
	Budget        string        `yaml:"budget"`
	SampleWindow  time.Duration `yaml:"sample_window"`
	ParallelWidth int           `yaml:"parallel_width"`
}
type DecisionConfig struct {
	Provider string        `yaml:"provider"`
	BaseURL  string        `yaml:"base_url"`
	Model    string        `yaml:"model"`
	APIKey   string        `yaml:"api_key"`
	Timeout  time.Duration `yaml:"timeout"`
}
type NarratorConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
}
type IdentityConfig struct {
	DockerSocket  string `yaml:"docker_socket"`
	CloudMetadata bool   `yaml:"cloud_metadata"`
}
type AgentConfig struct {
	ReportDir string `yaml:"report_dir"`
	Retain    int    `yaml:"retain"`
}

type Config struct {
	App struct {
		Name     string `yaml:"name"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"app"`

	AI struct {
		Provider string                 `yaml:"provider"`
		APIKey   string                 `yaml:"api_key"`
		BaseURL  string                 `yaml:"base_url"`
		Timeout  time.Duration          `yaml:"timeout"`
		Model    string                 `yaml:"model"`
		Models   map[string]ModelConfig `yaml:"models"`

		RequestLimits struct {
			MaxLinesContextFile int `yaml:"max_lines_context_file"`
		} `yaml:"request_limits"`
	} `yaml:"ai"`

	Logs struct {
		Applications []ApplicationLogConfig `yaml:"applications"`
	} `yaml:"logs"`

	Engine   EngineConfig   `yaml:"engine"`
	Decision DecisionConfig `yaml:"decision"`
	Narrator NarratorConfig `yaml:"narrator"`
	Identity IdentityConfig `yaml:"identity"`
	Agent    AgentConfig    `yaml:"agent"`

	Server struct {
		Listen string `yaml:"listen"`
	} `yaml:"server"`

	Output struct {
		ReportType string `yaml:"report_type"`
		Directory  string `yaml:"directory"`
	} `yaml:"output"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	if path == "" {
		path = DiscoverPath()
	}
	if path != "" {
		if resolved, err := filepath.Abs(path); err == nil {
			path = resolved
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}
	applyEnv(&cfg)

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// DiscoverPath returns the first conventional config file. An empty result is
// intentional: a default config plus environment/SSH-agent authentication is valid.
func DiscoverPath() string {
	// app.yaml is the production configuration. configs/app.yaml remains a
	// compatibility location for existing installations.
	candidates := []string{}
	// A downloaded release consists of only diagnos and app.yaml. Prefer the
	// adjacent config so running it from another working directory still works.
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}

		dir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(dir, "app.yaml"),
			filepath.Join(dir, "app.yml"),
		)
	}
	candidates = append(candidates, "app.yaml", "app.yml", filepath.Join("configs", "app.yaml"), filepath.Join("configs", "app.yml"))
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "diagnos", "config.yml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "diagnos", "config.yml"), filepath.Join(home, ".diagnos", "config.yml"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
func defaults() Config {
	var cfg Config
	cfg.App.Name = "diagnos"
	cfg.App.LogLevel = "info"
	// The direct report is intentionally compact; this is the shared evidence
	// budget used by both report modes.
	cfg.AI.RequestLimits.MaxLinesContextFile = 250
	cfg.Output.ReportType = "app-metrics"
	cfg.Output.Directory = "~/diagnos/reports"
	cfg.AI.Model = "gemini/gemini-3.5-flash-lite"
	cfg.Engine.Budget = "normal"
	cfg.Engine.SampleWindow = time.Second
	cfg.Engine.ParallelWidth = 3
	cfg.Decision.Provider = "jev"
	cfg.Decision.BaseURL = "https://api.typesafe.ai"
	cfg.Decision.Model = "jev-latest"
	cfg.Decision.Timeout = 10 * time.Second
	cfg.Narrator.Enabled = true
	cfg.Narrator.Model = cfg.AI.Model
	cfg.Identity.DockerSocket = "/var/run/docker.sock"
	cfg.Agent.ReportDir = "~/diagnos/reports"
	cfg.Server.Listen = "127.0.0.1:8080"
	cfg.Agent.Retain = 50
	cfg.AI.Models = map[string]ModelConfig{
		cfg.AI.Model: {BaseURL: "https://generativelanguage.googleapis.com", APIKeyEnv: "DIAGNOS_AI_API_KEY"},
	}
	return cfg
}
func applyEnv(cfg *Config) {
	if v := os.Getenv("TYPESAFE_API_KEY"); v != "" {
		cfg.Decision.APIKey = v
	}
	if v := os.Getenv("DIAGNOS_DECISION_API_KEY"); v != "" {
		cfg.Decision.APIKey = v
	}
	if v := os.Getenv("DIAGNOS_ENGINE_BUDGET"); v != "" {
		cfg.Engine.Budget = v
	}
	if v := os.Getenv("DIAGNOS_DECISION_PROVIDER"); v != "" {
		cfg.Decision.Provider = v
	}
	if v := os.Getenv("DIAGNOS_DECISION_MODEL"); v != "" {
		cfg.Decision.Model = v
	}
	if v := os.Getenv("DIAGNOS_DECISION_BASE_URL"); v != "" {
		cfg.Decision.BaseURL = v
	}
	if v := os.Getenv("DIAGNOS_AI_API_KEY"); v != "" { // existing model adapters use their named env var; retain the value for config check only.
		cfg.AI.APIKey = v
		if cfg.AI.Models == nil {
			cfg.AI.Models = map[string]ModelConfig{}
		}
		if cfg.AI.Model != "" {
			model := cfg.AI.Models[cfg.AI.Model]
			if model.APIKeyEnv == "" {
				model.APIKeyEnv = "DIAGNOS_AI_API_KEY"
				cfg.AI.Models[cfg.AI.Model] = model
			}
		}
	}
}

func ResolveOutputDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("output directory cannot be empty")
	}

	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}

	return filepath.Clean(path), nil
}
