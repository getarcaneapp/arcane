package projects

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/getarcaneapp/arcane/backend/internal/utils/fs"
	"github.com/getarcaneapp/arcane/types/project"
	"github.com/goccy/go-yaml"
)

var ReservedCustomFileNames = append(fs.ComposeFileCandidates, fs.EnvFileCandidates...)

const (
	// PlaceholderYAML is the placeholder content for new YAML files
	PlaceholderYAML = "# This file will be created when you save changes\nservices:\n"
	// PlaceholderGeneric is the placeholder content for new generic files
	PlaceholderGeneric = "# This file will be created when you save changes\n"
)

// Security Model for Include Files:
// - READ: Docker Compose allows include files from anywhere (parent dirs, absolute paths, etc.)
//         We allow reading from any path to maintain compatibility with standard Docker Compose behavior
// - WRITE: Restricted to files within the project directory or configured allowed external paths

// ParseIncludes reads a compose file and extracts all include directives.
func ParseIncludes(composeFilePath string) ([]project.IncludeFile, error) {
	content, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var composeData map[string]interface{}
	if err := yaml.Unmarshal(content, &composeData); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	includes, ok := composeData["include"]
	if !ok {
		return nil, nil
	}

	composeDir := filepath.Dir(composeFilePath)
	var files []project.IncludeFile

	parseItem := func(item interface{}) {
		inc, err := parseIncludeItem(item, composeDir)
		if err == nil {
			files = append(files, inc)
		}
	}

	switch v := includes.(type) {
	case []interface{}:
		for _, item := range v {
			parseItem(item)
		}
	case string:
		parseItem(v)
	}

	return files, nil
}

func parseIncludeItem(item interface{}, baseDir string) (project.IncludeFile, error) {
	var includePath string

	switch v := item.(type) {
	case string:
		includePath = v
	case map[string]interface{}:
		if path, ok := v["path"].(string); ok {
			includePath = path
		}
	default:
		return project.IncludeFile{}, fmt.Errorf("invalid include item type")
	}

	if includePath == "" {
		return project.IncludeFile{}, fmt.Errorf("empty include path")
	}

	fullPath := includePath
	if !filepath.IsAbs(includePath) {
		fullPath = filepath.Join(baseDir, includePath)
	}
	fullPath = filepath.Clean(fullPath)

	content, err := readFileWithPlaceholder(fullPath, PlaceholderYAML)
	if err != nil {
		return project.IncludeFile{}, fmt.Errorf("failed to read include file %s: %w", includePath, err)
	}

	relativePath := includePath
	if filepath.IsAbs(includePath) {
		if rel, err := filepath.Rel(baseDir, fullPath); err == nil {
			relativePath = rel
		}
	}

	return project.IncludeFile{
		Path:         fullPath,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}

func readFileWithPlaceholder(absPath, placeholder string) (string, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return placeholder, nil
		}
		return "", err
	}
	return string(content), nil
}
