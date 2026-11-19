package secretsource

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"getarcane.dev/types/v3"
)

func LoadPresets(fsys fs.FS, dir string) ([]types.SecretSourcePreset, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret source presets: %w", err)
	}

	presets := make([]types.SecretSourcePreset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		source, readErr := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("failed to read secret source preset %s: %w", entry.Name(), readErr)
		}
		definition, parseErr := ParseDefinition(source)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid secret source preset %s: %w", entry.Name(), parseErr)
		}
		presets = append(presets, types.SecretSourcePreset{
			Definition: definition,
			Source:     string(source),
		})
	}
	slices.SortFunc(presets, func(a, b types.SecretSourcePreset) int {
		return strings.Compare(strings.ToLower(a.Definition.Name), strings.ToLower(b.Definition.Name))
	})
	return presets, nil
}
