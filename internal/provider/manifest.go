package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ManifestDir = "/usr/share/pinproc/providers"
	BinaryDir   = "/usr/libexec/pinproc/providers"
)

var (
	providerIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	settingKeyRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// Setting describes one configuration value requested by a provider.
type Setting struct {
	Key         string `yaml:"key" json:"key"`
	Label       string `yaml:"label" json:"label"`
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required" json:"required"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
}

// Manifest describes an installed provider without executing it.
type Manifest struct {
	ProtocolVersion int       `yaml:"protocol_version" json:"protocol_version"`
	ID              string    `yaml:"id" json:"id"`
	Name            string    `yaml:"name" json:"name"`
	Description     string    `yaml:"description,omitempty" json:"description,omitempty"`
	Executable      string    `yaml:"executable" json:"executable"`
	Settings        []Setting `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// LoadManifest reads and validates one provider manifest.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode provider manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// List returns all valid installed provider manifests.
func List(dir string) ([]Manifest, error) {
	if strings.TrimSpace(dir) == "" {
		dir = ManifestDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Manifest, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		m, err := LoadManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("load provider manifest %s: %w", entry.Name(), err)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find returns one provider manifest by ID.
func Find(id string) (Manifest, error) {
	if !providerIDRE.MatchString(id) {
		return Manifest{}, fmt.Errorf("invalid provider id %q", id)
	}
	manifests, err := List(ManifestDir)
	if err != nil {
		return Manifest{}, err
	}
	for _, m := range manifests {
		if m.ID == id {
			return m, nil
		}
	}
	return Manifest{}, fmt.Errorf("provider %q is not installed", id)
}

// BinaryInstalled reports whether the provider executable exists and is executable.
func BinaryInstalled(m Manifest, dir string) bool {
	info, err := os.Stat(m.BinaryPath(dir))
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func (m Manifest) Validate() error {
	if m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("provider %q uses unsupported protocol version %d", m.ID, m.ProtocolVersion)
	}
	if !providerIDRE.MatchString(m.ID) {
		return fmt.Errorf("provider id %q is invalid", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("provider %q has no name", m.ID)
	}
	if strings.TrimSpace(m.Executable) == "" || filepath.Base(m.Executable) != m.Executable {
		return fmt.Errorf("provider %q executable must be a plain file name", m.ID)
	}
	seen := map[string]bool{}
	for _, s := range m.Settings {
		if !settingKeyRE.MatchString(s.Key) {
			return fmt.Errorf("provider %q has invalid setting key %q", m.ID, s.Key)
		}
		if seen[s.Key] {
			return fmt.Errorf("provider %q has duplicate setting %q", m.ID, s.Key)
		}
		seen[s.Key] = true
		switch s.Type {
		case "string", "secret", "url", "boolean":
		default:
			return fmt.Errorf("provider %q setting %q has unsupported type %q", m.ID, s.Key, s.Type)
		}
	}
	return nil
}

// BinaryPath returns the executable path for an installed provider.
func (m Manifest) BinaryPath(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = BinaryDir
	}
	return filepath.Join(dir, m.Executable)
}
