package config

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ManagedPath = "/etc/pinproc/config.yaml"
	StateDir    = "/var/lib/pinproc"
)

type AIConfig struct {
	Enabled  bool           `yaml:"enabled"`
	Provider string         `yaml:"provider,omitempty"`
	Config   map[string]any `yaml:"config,omitempty"`
}

type SourceConfig struct {
	ReadTimeout time.Duration `yaml:"read_timeout"`
}

type NarratorConfig struct {
	Enabled bool `yaml:"enabled"`
}

type CallbackConfig struct {
	Enabled bool          `yaml:"enabled"`
	URL     string        `yaml:"url,omitempty"`
	Timeout time.Duration `yaml:"timeout"`
}

type Config struct {
	App struct {
		Name     string `yaml:"name"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"app"`

	Server struct {
		Listen string `yaml:"listen"`
		APIKey string `yaml:"api_key,omitempty"`
	} `yaml:"server"`

	AI       AIConfig       `yaml:"ai"`
	Narrator NarratorConfig `yaml:"narrator"`
	Source   SourceConfig   `yaml:"source"`
	Report   struct {
		MaxFindings int `yaml:"max_findings"`
	} `yaml:"report"`
	Callback CallbackConfig `yaml:"callback"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	if path == "" {
		path = DiscoverPath()
	}
	if path != "" {
		resolved, err := filepath.Abs(path)
		if err == nil {
			path = resolved
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return &cfg, nil
			}
			return nil, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}
	normalize(&cfg)
	applyEnv(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}

func DiscoverPath() string {
	return ManagedPath
}

func defaults() Config {
	var cfg Config
	cfg.App.Name = "pinproc"
	cfg.App.LogLevel = "info"
	cfg.Server.Listen = "127.0.0.1:8080"
	cfg.AI.Enabled = false
	cfg.AI.Config = map[string]any{}
	cfg.Narrator.Enabled = true
	cfg.Source.ReadTimeout = 2 * time.Second
	cfg.Report.MaxFindings = 5
	cfg.Callback.Timeout = 5 * time.Second
	return cfg
}

func normalize(cfg *Config) {
	cfg.AI.Provider = strings.TrimSpace(cfg.AI.Provider)
	if cfg.AI.Config == nil {
		cfg.AI.Config = map[string]any{}
	}
}

func applyEnv(cfg *Config) {}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(cfg.App.Name) == "" {
		cfg.App.Name = "pinproc"
	}
	if cfg.Source.ReadTimeout <= 0 {
		cfg.Source.ReadTimeout = 2 * time.Second
	}
	if cfg.Report.MaxFindings < 1 {
		cfg.Report.MaxFindings = 5
	}
	if cfg.Callback.Timeout <= 0 {
		cfg.Callback.Timeout = 5 * time.Second
	}
	if cfg.Callback.Timeout > 30*time.Second {
		cfg.Callback.Timeout = 30 * time.Second
	}
	if strings.TrimSpace(cfg.Server.Listen) == "" {
		cfg.Server.Listen = "127.0.0.1:8080"
	}
	if cfg.AI.Enabled && cfg.AI.Provider == "" {
		return fmt.Errorf("ai.provider is required when ai.enabled is true")
	}
	if cfg.Callback.Enabled && strings.TrimSpace(cfg.Callback.URL) == "" {
		return fmt.Errorf("callback.url is required when callback.enabled is true")
	}
	if !isLoopbackListen(cfg.Server.Listen) && strings.TrimSpace(cfg.Server.APIKey) == "" {
		return fmt.Errorf("non-loopback server.listen requires server.api_key")
	}
	return nil
}

func SaveManaged(cfg *Config) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("managed configuration must be changed as root")
	}
	return Save(ManagedPath, cfg)
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pinproc-config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := setConfigOwnership(name); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func setConfigOwnership(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	group, err := user.LookupGroup("pinproc")
	if err != nil {
		return nil
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return fmt.Errorf("set config ownership: %w", err)
	}
	return nil
}

func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
