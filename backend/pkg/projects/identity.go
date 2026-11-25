package projects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/compose-spec/compose-go/v2/consts"
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app/acfs"
)

// ComposeProjectNameRewrite points a project's literal Compose name sources at Name.
type ComposeProjectNameRewrite struct {
	ProjectsDirectory string
	ProjectPath       string
	ComposeFile       string
	Name              string
}

// SetComposeProjectName replaces the value of every top-level `name` in source,
// leaving all other bytes untouched. Sources without one are returned as is.
func SetComposeProjectName(source []byte, name string) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	var edits []ScalarEdit
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("parse Compose source: %w", err)
		}
		if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
			continue
		}
		mapping := document.Content[0]
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			value := mapping.Content[i+1]
			if mapping.Content[i].Value != "name" || value.Tag == "!!null" || value.Value == name {
				continue
			}
			if value.Kind != yaml.ScalarNode || value.Anchor != "" {
				return nil, errors.New("top-level Compose name must be a plain scalar")
			}
			edit, err := NewScalarEdit(source, value, name)
			if err != nil {
				return nil, fmt.Errorf("top-level Compose name: %w", err)
			}
			edits = append(edits, edit)
		}
	}
	return ApplyScalarEdits(source, edits), nil
}

// RewriteComposeProjectName sets the top-level `name` of every Compose file the
// project loads and COMPOSE_PROJECT_NAME in its .env, when present, to the
// requested name. Files outside the project directory are left alone.
func RewriteComposeProjectName(ctx context.Context, rewrite ComposeProjectNameRewrite) error {
	projectRoot, err := filepath.EvalSymlinks(rewrite.ProjectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	files, err := ComposeFileEnvSelection(ctx, rewrite.ProjectsDirectory, rewrite.ProjectPath)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		files = []string{rewrite.ComposeFile}
		if slices.Contains(ComposeFileCandidates, filepath.Base(rewrite.ComposeFile)) {
			if overrideFile := DetectComposeOverrideFile(ctx, rewrite.ProjectPath); overrideFile != "" {
				files = append(files, overrideFile)
			}
		}
	}
	for _, file := range files {
		if !IsSafeSubdirectory(rewrite.ProjectPath, file) {
			continue
		}
		resolvedFile, resolveErr := filepath.EvalSymlinks(file)
		if resolveErr != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(file), resolveErr)
		}
		if resolvedFile == projectRoot || !IsSafeSubdirectory(projectRoot, resolvedFile) {
			continue
		}
		source, readErr := acfs.Host{}.ReadFile(ctx, resolvedFile)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(file), readErr)
		}
		updated, setErr := SetComposeProjectName(source, rewrite.Name)
		if setErr != nil {
			return fmt.Errorf("%s: %w", filepath.Base(file), setErr)
		}
		if bytes.Equal(updated, source) {
			continue
		}
		if writeErr := WriteProjectFile(ctx, projectRoot, filepath.Dir(resolvedFile), filepath.Base(resolvedFile), string(updated)); writeErr != nil {
			return writeErr
		}
	}

	resolvedEnv, err := filepath.EvalSymlinks(filepath.Join(rewrite.ProjectPath, EffectiveEnvFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", EffectiveEnvFileName, err)
	}
	if resolvedEnv == projectRoot || !IsSafeSubdirectory(projectRoot, resolvedEnv) {
		return nil
	}
	envContent, err := acfs.Host{}.ReadFile(ctx, resolvedEnv)
	if err != nil {
		return fmt.Errorf("read %s: %w", EffectiveEnvFileName, err)
	}
	env, err := ParseProjectEnvContent(string(envContent), nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", EffectiveEnvFileName, err)
	}
	if current, ok := env[consts.ComposeProjectName]; !ok || current == rewrite.Name {
		return nil
	}
	updated, err := BuildEffectiveEnvContent(string(envContent), consts.ComposeProjectName+"="+rewrite.Name+"\n", nil)
	if err != nil {
		return fmt.Errorf("rewrite %s: %w", EffectiveEnvFileName, err)
	}
	return WriteProjectFile(ctx, projectRoot, filepath.Dir(resolvedEnv), filepath.Base(resolvedEnv), updated)
}
