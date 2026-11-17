package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/monitor"
	"gopkg.in/yaml.v3"
)

const (
	ConfigPath    = "/etc/pinproc/config.yaml"
	DataDirectory = "/var/lib/pinproc"
	ServiceUser   = "pinproc"
	ServiceGroup  = "pinproc"
)

type SourceConfig struct {
	ReadTimeout time.Duration `yaml:"read_timeout"`
}
type NarratorConfig struct {
	Enabled bool `yaml:"enabled"`
}
type CallbackConfig struct {
	Enabled bool          `yaml:"enabled"`
	URL     string        `yaml:"url"`
	Method  string        `yaml:"method"` // POST (default) or GET; the report is the request body either way
	Format  string        `yaml:"format"` // html (default) | json | text
	Timeout time.Duration `yaml:"timeout"`
}

// EmailConfig is an SMTP account the operator brings. pinproc ships no mail
// provider: it speaks plain SMTP (net/smtp) to the server configured here.
type EmailConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	Security     string        `yaml:"security"` // starttls (587, default) | tls (465) | none (local relay)
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password,omitempty"`
	PasswordFile string        `yaml:"password_file,omitempty"`
	From         string        `yaml:"from"`
	To           []string      `yaml:"to"`
	Timeout      time.Duration `yaml:"timeout"`
	AttachReport bool          `yaml:"attach_report"` // attach the full HTML page to each email
}

// AlertsConfig controls automatic delivery of captured incidents.
type AlertsConfig struct {
	// Cooldown is the minimum gap between two automatic alerts for the same
	// resource on the same channel, so a long incident is reported once, not on
	// every capture. Manual sends (pinproc alert / report) ignore it.
	Cooldown time.Duration `yaml:"cooldown"`
}

type Config struct {
	Version int `yaml:"version"`
	App     struct {
		Name     string `yaml:"name"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"app"`
	Narrator NarratorConfig `yaml:"narrator"`
	Source   SourceConfig   `yaml:"source"`
	Report   struct {
		MaxFindings int    `yaml:"max_findings"`
		Timezone    string `yaml:"timezone"` // UTC (default), IST, an IANA name, or +05:30
		Tag         string `yaml:"tag"`      // optional label in report headers, e.g. "production"
	} `yaml:"report"`
	Callback CallbackConfig `yaml:"callback"`
	Email    EmailConfig    `yaml:"email"`
	Alerts   AlertsConfig   `yaml:"alerts"`
	Monitor  monitor.Config `yaml:"monitor"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	explicitPath := path != ""
	if path == "" {
		path = DiscoverPath()
	}
	if path != "" {
		if resolved, err := filepath.Abs(path); err == nil {
			path = resolved
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && !explicitPath {
				return &cfg, nil
			}
			return nil, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}
	normalize(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if path == "" {
		path = ConfigPath
	}
	if err := Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if g, err := user.LookupGroup(ServiceGroup); err == nil {
			if gid, err := strconv.Atoi(g.Gid); err == nil {
				_ = os.Chown(tmpName, 0, gid)
			}
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	_ = os.Chmod(path, 0640)
	return nil
}

func DiscoverPath() string {
	candidates := []string{ConfigPath, "config.yaml", "config.yml", "app.yaml", "app.yml"}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		dir := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(dir, "config.yaml"), filepath.Join(dir, "config.yml"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "pinproc", "config.yaml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "pinproc", "config.yaml"))
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
	cfg.Version = 1
	cfg.App.Name = "pinproc"
	cfg.App.LogLevel = "info"
	cfg.Narrator.Enabled = true
	cfg.Source.ReadTimeout = 2 * time.Second
	cfg.Report.MaxFindings = 5
	cfg.Report.Timezone = "UTC"
	cfg.Callback.Method = "POST"
	cfg.Callback.Format = "html"
	cfg.Callback.Timeout = 5 * time.Second
	cfg.Email.Port = 587
	cfg.Email.Security = "starttls"
	cfg.Email.Timeout = 15 * time.Second
	cfg.Email.AttachReport = true
	cfg.Alerts.Cooldown = 30 * time.Minute
	return cfg
}

// EmailPassword returns the SMTP password, from password_file when set.
func (e EmailConfig) EmailPassword() (string, error) {
	if e.PasswordFile != "" {
		b, err := os.ReadFile(e.PasswordFile)
		if err != nil {
			return "", fmt.Errorf("email.password_file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return e.Password, nil
}

func normalize(cfg *Config) {
	cfg.Monitor.Normalize()
}

func ResolveOutputDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return DataDirectory, nil
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
