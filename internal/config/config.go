package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/monitor"
	"gopkg.in/yaml.v3"
)

const (
	ConfigPath = "/etc/pinproc/config.yaml"
	DataDirectory = "/var/lib/pinproc"
	ProviderDir = "/usr/libexec/pinproc/providers"
	ProviderManifests = "/usr/share/pinproc/providers"
	ServiceUser = "pinproc"
	ServiceGroup = "pinproc"
)

type AIConfig struct {
	Provider string `yaml:"provider"`
	Config map[string]string `yaml:"config"`
}

type SourceConfig struct { ReadTimeout time.Duration `yaml:"read_timeout"` }
type NarratorConfig struct { Enabled bool `yaml:"enabled"` }
type CallbackConfig struct {
	Enabled bool `yaml:"enabled"`
	URL string `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"`
}

type Config struct {
	Version int `yaml:"version"`
	App struct { Name string `yaml:"name"`; LogLevel string `yaml:"log_level"` } `yaml:"app"`
	AI AIConfig `yaml:"ai"`
	Narrator NarratorConfig `yaml:"narrator"`
	Source SourceConfig `yaml:"source"`
	Report struct { MaxFindings int `yaml:"max_findings"` } `yaml:"report"`
	Server struct { Listen string `yaml:"listen"`; APIKey string `yaml:"api_key"` } `yaml:"server"`
	Callback CallbackConfig `yaml:"callback"`
	Monitor monitor.Config `yaml:"monitor"`
}

var providerIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func Load(path string) (*Config, error) {
	cfg := defaults()
	explicitPath := path != ""
	if path == "" { path = DiscoverPath() }
	if path != "" {
		if resolved, err := filepath.Abs(path); err == nil { path = resolved }
		data, err := os.ReadFile(path)
		if err != nil { if os.IsNotExist(err) && !explicitPath { return &cfg, nil }; return nil, err }
		if err := yaml.Unmarshal(data, &cfg); err != nil { return nil, err }
	}
	normalize(&cfg)
	if err := Validate(&cfg); err != nil { return nil, fmt.Errorf("validate config: %w", err) }
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg == nil { return fmt.Errorf("config is nil") }
	if path == "" { path = ConfigPath }
	if err := Validate(cfg); err != nil { return fmt.Errorf("validate config: %w", err) }
	data, err := yaml.Marshal(cfg); if err != nil { return fmt.Errorf("marshal config: %w", err) }
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil { return fmt.Errorf("create config directory: %w", err) }
	tmp, err := os.CreateTemp(dir, ".config-*.tmp"); if err != nil { return fmt.Errorf("create temporary config: %w", err) }
	tmpName := tmp.Name(); defer os.Remove(tmpName)
	if err := tmp.Chmod(0640); err != nil { _ = tmp.Close(); return err }
	if _, err := tmp.Write(data); err != nil { _ = tmp.Close(); return fmt.Errorf("write temporary config: %w", err) }
	if err := tmp.Sync(); err != nil { _ = tmp.Close(); return fmt.Errorf("sync temporary config: %w", err) }
	if err := tmp.Close(); err != nil { return err }
	if os.Geteuid() == 0 {
		if g, err := user.LookupGroup(ServiceGroup); err == nil { if gid, err := strconv.Atoi(g.Gid); err == nil { _ = os.Chown(tmpName, 0, gid) } }
	}
	if err := os.Rename(tmpName, path); err != nil { return fmt.Errorf("install config: %w", err) }
	_ = os.Chmod(path, 0640)
	return nil
}

func DiscoverPath() string {
	candidates := []string{ConfigPath, "config.yaml", "config.yml", "app.yaml", "app.yml"}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil { executable = resolved }
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, "config.yaml"), filepath.Join(dir, "config.yml"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" { candidates = append(candidates, filepath.Join(xdg, "pinproc", "config.yaml")) } else if home, err := os.UserHomeDir(); err == nil { candidates = append(candidates, filepath.Join(home, ".config", "pinproc", "config.yaml")) }
	for _, candidate := range candidates { if _, err := os.Stat(candidate); err == nil { return candidate } }
	return ""
}

func defaults() Config {
	var cfg Config
	cfg.Version = 1
	cfg.App.Name = "pinproc"
	cfg.App.LogLevel = "info"
	cfg.AI.Config = map[string]string{}
	cfg.Narrator.Enabled = true
	cfg.Source.ReadTimeout = 2 * time.Second
	cfg.Report.MaxFindings = 5
	cfg.Server.Listen = "127.0.0.1:8080"
	cfg.Callback.Timeout = 5 * time.Second
	return cfg
}

func normalize(cfg *Config) {
	cfg.AI.Provider = strings.TrimSpace(cfg.AI.Provider)
	if cfg.AI.Config == nil { cfg.AI.Config = map[string]string{} }
	cfg.Monitor.Normalize()
}

func ValidProviderID(id string) bool { return providerIDRE.MatchString(strings.TrimSpace(id)) }

func ResolveOutputDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" { return DataDirectory, nil }
	if path == "~" { home, err := os.UserHomeDir(); if err != nil { return "", fmt.Errorf("resolve home directory: %w", err) }; return home, nil }
	if strings.HasPrefix(path, "~/") { home, err := os.UserHomeDir(); if err != nil { return "", fmt.Errorf("resolve home directory: %w", err) }; return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil }
	return filepath.Clean(path), nil
}