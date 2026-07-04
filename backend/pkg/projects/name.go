package projects

import (
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	"go.yaml.in/yaml/v4"
)

// ComposeContentProjectName returns the normalized top-level `name:` from
// compose YAML content, or "" when the key is absent or unusable. Interpolated
// names (containing `${`) are treated as absent so the backend and the
// frontend name lock behave identically; parse errors are ignored here because
// the full compose validation reports them with proper context.
func ComposeContentProjectName(composeContent string) string {
	var doc struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(composeContent), &doc); err != nil {
		return ""
	}
	raw := strings.TrimSpace(doc.Name)
	if raw == "" || strings.Contains(raw, "${") {
		return ""
	}
	return loader.NormalizeProjectName(raw)
}

// NormalizeProjectName returns compose-go's normalized project name. If
// compose-go returns empty, the input is returned unchanged.
func NormalizeProjectName(name string) string {
	if name == "" {
		return ""
	}
	normalized := loader.NormalizeProjectName(name)
	if normalized == "" {
		return name
	}
	return normalized
}
