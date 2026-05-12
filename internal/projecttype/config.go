package projecttype

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the YAML schema accepted by LoadFromFile. Each entry
// registers a new ProjectType alongside the built-ins.
//
//	project_types:
//	  - name: my-app
//	    description: Internal Foo app
//	    indicators:
//	      - cel: '"services" in subdirs && "foo.yaml" in files'
//	  - name: helm-chart
//	    indicators:
//	      - has_file: Chart.yaml
//	      - has_file: values.yaml
type Config struct {
	ProjectTypes []ConfigEntry `yaml:"project_types"`
}

// ConfigEntry is one project type declared in the config file.
type ConfigEntry struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Indicators  []ConfigIndicator `yaml:"indicators"`
}

// ConfigIndicator is the YAML representation of an Indicator. Exactly
// one of HasFile / HasGlob / CEL should be set per entry.
type ConfigIndicator struct {
	HasFile string `yaml:"has_file,omitempty"`
	HasGlob string `yaml:"has_glob,omitempty"`
	CEL     string `yaml:"cel,omitempty"`
}

// LoadFromFile parses path as YAML and registers every project type
// into this registry. Validates each entry (non-empty name + at
// least one indicator) and surfaces CEL compile errors with the
// offending entry's name. Returns the number of types registered.
//
// Idempotent: calling LoadFromFile twice with the same config
// registers the types twice (caller's responsibility to avoid).
func (r *Registry) LoadFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("project-type config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Errorf("project-type config YAML: %w", err)
	}
	registered := 0
	for i, entry := range cfg.ProjectTypes {
		pt, err := buildProjectType(entry)
		if err != nil {
			return registered, fmt.Errorf("project-type config entry %d (%q): %w", i, entry.Name, err)
		}
		if err := r.Register(pt); err != nil {
			return registered, fmt.Errorf("project-type config entry %d (%q): %w", i, entry.Name, err)
		}
		registered++
	}
	return registered, nil
}

// LoadFromFile is the package-level shortcut that targets
// DefaultRegistry(). The CLI's --project-type-config flag uses this
// directly.
func LoadFromFile(path string) (int, error) {
	return defaultRegistry.LoadFromFile(path)
}

// buildProjectType validates a ConfigEntry and converts it to a
// *ProjectType ready for Register. CEL compilation is deferred to
// Register so failures surface uniformly with the panic-recovery
// path in tryRegister.
func buildProjectType(entry ConfigEntry) (*ProjectType, error) {
	if entry.Name == "" {
		return nil, errors.New("name is required")
	}
	if len(entry.Indicators) == 0 {
		return nil, errors.New("at least one indicator is required")
	}
	inds := make([]Indicator, 0, len(entry.Indicators))
	for j, ci := range entry.Indicators {
		switch {
		case ci.HasFile != "":
			inds = append(inds, Indicator{HasFile: ci.HasFile})
		case ci.HasGlob != "":
			inds = append(inds, Indicator{HasGlob: ci.HasGlob})
		case ci.CEL != "":
			inds = append(inds, Indicator{CELExpr: ci.CEL})
		default:
			return nil, fmt.Errorf("indicator[%d]: must set has_file / has_glob / cel", j)
		}
	}
	return &ProjectType{
		Name:        entry.Name,
		Description: entry.Description,
		Indicators:  inds,
	}, nil
}

