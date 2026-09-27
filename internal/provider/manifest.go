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

const ProtocolVersion = 1

type Setting struct {
	Name string `yaml:"name"`
	Label string `yaml:"label"`
	Type string `yaml:"type"`
	Required bool `yaml:"required"`
	Description string `yaml:"description,omitempty"`
	Default string `yaml:"default,omitempty"`
}

type Manifest struct {
	ProtocolVersion int `yaml:"protocol_version"`
	ID string `yaml:"id"`
	Name string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Executable string `yaml:"executable"`
	Settings []Setting `yaml:"settings,omitempty"`
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil { return Manifest{}, err }
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil { return Manifest{}, fmt.Errorf("decode provider manifest: %w", err) }
	if err := Validate(m); err != nil { return Manifest{}, err }
	return m, nil
}

func LoadInstalled(id string) (Manifest, error) {
	id = strings.TrimSpace(id)
	if !idRE.MatchString(id) { return Manifest{}, fmt.Errorf("invalid provider id %q", id) }
	m, err := Load(filepath.Join("/usr/share/pinproc/providers", id+".yaml"))
	if err != nil { return Manifest{}, fmt.Errorf("provider %q is not installed: %w", id, err) }
	if m.Executable == "" { m.Executable = filepath.Join("/usr/libexec/pinproc/providers", id) }
	if _, err := os.Stat(m.Executable); err != nil { return Manifest{}, fmt.Errorf("provider %q executable is unavailable: %w", id, err) }
	return m, nil
}

func Discover(manifestDir string) ([]Manifest, error) {
	if manifestDir == "" { manifestDir = "/usr/share/pinproc/providers" }
	matches, err := filepath.Glob(filepath.Join(manifestDir, "*.yaml"))
	if err != nil { return nil, err }
	sort.Strings(matches)
	out := make([]Manifest, 0, len(matches))
	for _, path := range matches {
		m, err := Load(path)
		if err != nil { continue }
		if m.Executable == "" { m.Executable = filepath.Join("/usr/libexec/pinproc/providers", m.ID) }
		out = append(out, m)
	}
	return out, nil
}

func Validate(m Manifest) error {
	if m.ProtocolVersion != ProtocolVersion { return fmt.Errorf("provider %q uses unsupported protocol version %d", m.ID, m.ProtocolVersion) }
	if !idRE.MatchString(m.ID) { return fmt.Errorf("provider id %q is invalid", m.ID) }
	if strings.TrimSpace(m.Name) == "" { return fmt.Errorf("provider %q has no display name", m.ID) }
	if strings.TrimSpace(m.Executable) == "" { return fmt.Errorf("provider %q has no executable", m.ID) }
	if !filepath.IsAbs(m.Executable) { return fmt.Errorf("provider %q executable must be an absolute path", m.ID) }
	seen := map[string]bool{}
	for _, s := range m.Settings {
		if !idRE.MatchString(s.Name) { return fmt.Errorf("provider %q setting %q has invalid name", m.ID, s.Name) }
		switch s.Type { case "string", "url", "secret": default: return fmt.Errorf("provider %q setting %q has unsupported type %q", m.ID, s.Name, s.Type) }
		if seen[s.Name] { return fmt.Errorf("provider %q defines setting %q more than once", m.ID, s.Name) }
		seen[s.Name] = true
	}
	return nil
}