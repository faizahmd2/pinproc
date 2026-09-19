package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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
	Enabled bool `yaml:"enabled"`
}

type AgentConfig struct {
	ReportDir string `yaml:"report_dir"`
}

type Config struct {
	App struct {
		Name     string `yaml:"name"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"app"`

	Engine   EngineConfig   `yaml:"engine"`
	Decision DecisionConfig `yaml:"decision"`
	Narrator NarratorConfig `yaml:"narrator"`
	Agent    AgentConfig    `yaml:"agent"`

	Server struct {
		Listen string `yaml:"listen"`
	} `yaml:"server"`

	Output struct {
		Directory string `yaml:"directory"`
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

// DiscoverPath returns the first conventional configuration file.
func DiscoverPath() string {
	candidates := []string{}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, "app.yaml"), filepath.Join(dir, "app.yml"))
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
	cfg.Output.Directory = "~/diagnos/reports"
	cfg.Engine.Budget = "normal"
	cfg.Engine.SampleWindow = time.Second
	cfg.Engine.ParallelWidth = 3
	cfg.Decision.Provider = "jev"
	cfg.Decision.BaseURL = "https://api.typesafe.ai"
	cfg.Decision.Model = "jev-latest"
	cfg.Decision.Timeout = 10 * time.Second
	cfg.Narrator.Enabled = true
	cfg.Agent.ReportDir = "~/diagnos/reports"
	cfg.Server.Listen = "127.0.0.1:8080"
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
