package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	FilePerm = 0644
	DirPerm  = 0755
)

var composeFileCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// detectExistingComposeFile finds an existing compose file in the directory
func detectExistingComposeFile(dir string) string {
	for _, filename := range composeFileCandidates {
		fullPath := filepath.Join(dir, filename)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return fullPath
		}
	}
	return ""
}

// WriteComposeFile writes a compose file to the specified directory.
// It detects existing compose file names (docker-compose.yml, compose.yaml, etc.)
// and uses the existing name if found, otherwise defaults to compose.yaml
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteComposeFile(projectsRoot, dirPath, content string) error {
	// Security: Validate dirPath is absolute and clean to prevent path traversal
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}
	dirPath = filepath.Clean(absPath)

	// Security: Validate dirPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, dirPath) {
		return fmt.Errorf("refusing to write compose file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var composePath string
	if existingFile := detectExistingComposeFile(dirPath); existingFile != "" {
		composePath = existingFile
	} else {
		composePath = filepath.Join(dirPath, "compose.yaml")
	}

	if err := os.WriteFile(composePath, []byte(content), FilePerm); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

// WriteEnvFile writes a .env file to the specified directory
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteEnvFile(projectsRoot, dirPath, content string) error {
	// Security: Validate dirPath is absolute and clean to prevent path traversal
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}
	dirPath = filepath.Clean(absPath)

	// Security: Validate dirPath is within projectsRoot
	rootAbs, err := filepath.Abs(projectsRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, dirPath) {
		return fmt.Errorf("refusing to write env file: path outside projects root")
	}

	if err := os.MkdirAll(dirPath, DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	envPath := filepath.Join(dirPath, ".env")
	if err := os.WriteFile(envPath, []byte(content), FilePerm); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	return nil
}

// WriteProjectFiles writes both compose and env files to a project directory.
// An empty .env file is always created to prevent compose-go from failing when
// the compose file references env_file: .env
// projectsRoot is the allowed root directory to prevent path traversal attacks
func WriteProjectFiles(projectsRoot, dirPath, composeContent string, envContent *string) error {
	if err := WriteComposeFile(projectsRoot, dirPath, composeContent); err != nil {
		return err
	}

	// Always create a .env file, even if empty, to satisfy compose files that reference env_file: .env
	envValue := ""
	if envContent != nil {
		envValue = *envContent
	}
	if err := WriteEnvFile(projectsRoot, dirPath, envValue); err != nil {
		return err
	}

	return nil
}

// WriteTemplateFile writes a template file (like .compose.template or .env.template)
func WriteTemplateFile(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), FilePerm); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// WriteFileWithPerm is a generic file writer with custom permissions
func WriteFileWithPerm(filePath, content string, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
