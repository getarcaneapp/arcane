package volumes

import (
	"errors"
	"fmt"
	"os"
	"strings"

	composetemplate "github.com/compose-spec/compose-go/v2/template"
	"go.yaml.in/yaml/v4"
)

func composeVolumeKeysWithExplicitNameInternal(composeFiles []string) (map[string]struct{}, error) {
	explicit := make(map[string]struct{})
	for _, composeFile := range composeFiles {
		composeFile = strings.TrimSpace(composeFile)
		if composeFile == "" {
			continue
		}
		keys, err := composeVolumeKeysWithExplicitNameInFileInternal(composeFile)
		if err != nil {
			return nil, err
		}
		for key := range keys {
			explicit[key] = struct{}{}
		}
	}
	return explicit, nil
}

func composeVolumeKeysWithExplicitNameInFileInternal(path string) (map[string]struct{}, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}

	composeData := map[string]any{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}

	rawVolumes, ok := composeData["volumes"]
	if !ok || rawVolumes == nil {
		return map[string]struct{}{}, nil
	}

	volumes, ok := rawVolumes.(map[string]any)
	if !ok {
		return nil, errors.New("parse compose file: volumes must be a mapping")
	}

	explicit := make(map[string]struct{})
	for key, rawVolume := range volumes {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		volumeConfig, ok := rawVolume.(map[string]any)
		if !ok {
			continue
		}
		rawName, hasName := volumeConfig["name"]
		if !hasName {
			continue
		}
		name, ok := rawName.(string)
		if ok && len(composetemplate.ExtractVariables(map[string]any{"name": name}, nil)) == 0 {
			explicit[key] = struct{}{}
		}
	}

	return explicit, nil
}
