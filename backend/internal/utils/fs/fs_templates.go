package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func ReadFolderComposeTemplate(baseDir, folder string) (string, *string, string, bool, error) {
	composePath := filepath.Join(baseDir, folder, "compose.yaml")
	if _, err := os.Stat(composePath); err != nil {
		if os.IsNotExist(err) {
			return "", nil, "", false, nil
		}
		return "", nil, "", false, fmt.Errorf("stat compose: %w", err)
	}

	b, err := os.ReadFile(composePath)
	if err != nil {
		return "", nil, "", false, fmt.Errorf("read compose %s: %w", composePath, err)
	}

	var envPtr *string
	for _, envName := range []string{".env.example", ".env"} {
		envPath := filepath.Join(baseDir, folder, envName)
		if eb, err := os.ReadFile(envPath); err == nil {
			env := string(eb)
			envPtr = &env
			break
		}
	}

	desc := fmt.Sprintf("Imported from %s/%s/compose.yaml", baseDir, folder)
	return string(b), envPtr, desc, true, nil
}

func Slugify(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	in = strings.ReplaceAll(in, " ", "-")
	re := regexp.MustCompile(`[^a-z0-9\-_]+`)
	in = re.ReplaceAllString(in, "-")
	in = regexp.MustCompile(`-+`).ReplaceAllString(in, "-")
	return strings.Trim(in, "-")
}

func EnsureTemplateDir(ctx context.Context, base string) (dir, composePath, envPath string, err error) {
	baseDir, derr := GetTemplatesDirectory(ctx)
	if derr != nil {
		return "", "", "", fmt.Errorf("ensure templates dir: %w", derr)
	}
	dir = filepath.Join(baseDir, base)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", "", fmt.Errorf("failed to create template directory: %w", err)
	}
	composePath = filepath.Join(dir, "compose.yaml")
	envPath = filepath.Join(dir, ".env.example")
	return dir, composePath, envPath, nil
}

func ImportedComposeDescription(dir string) string {
	return fmt.Sprintf("Imported from %s/compose.yaml", dir)
}

func WriteTemplateFiles(composePath, envPath, composeContent, envContent string) (*string, error) {
	if err := WriteTemplateFile(composePath, composeContent); err != nil {
		return nil, err
	}

	envTrim := strings.TrimSpace(envContent)
	if envTrim == "" {
		return nil, nil
	}

	if err := WriteTemplateFile(envPath, envContent); err != nil {
		return nil, err
	}
	return &envContent, nil
}

func EnsureDefaultTemplates(ctx context.Context) error {
	templatesDir, err := GetTemplatesDirectory(ctx)
	if err != nil {
		return fmt.Errorf("get templates directory: %w", err)
	}

	composePath := filepath.Join(templatesDir, ".compose.template")
	envPath := filepath.Join(templatesDir, ".env.template")

	// Write default compose template if it doesn't exist
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		if err := WriteTemplateFile(composePath, getDefaultComposeTemplate()); err != nil {
			return fmt.Errorf("write default compose template: %w", err)
		}
	}

	// Write default env template if it doesn't exist
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := WriteTemplateFile(envPath, getDefaultEnvTemplate()); err != nil {
			return fmt.Errorf("write default env template: %w", err)
		}
	}

	return nil
}

func getDefaultComposeTemplate() string {
	return `services:
  nginx:
    image: nginx:alpine
    container_name: nginx_service
    env_file:
      - path: .env
        required: false
    ports:
      - "8080:80"
    volumes:
      - nginx_data:/usr/share/nginx/html
    restart: unless-stopped

volumes:
  nginx_data:
    driver: local
`
}

func getDefaultEnvTemplate() string {
	return `# Environment Variables
# These variables will be available to your project services
# Format: VARIABLE_NAME=value

# Web Server Configuration
NGINX_HOST=localhost
NGINX_PORT=80

# Database Configuration
POSTGRES_DB=myapp
POSTGRES_USER=myuser
POSTGRES_PASSWORD=mypassword
POSTGRES_PORT=5432

# Example Additional Variables
# API_KEY=your_api_key_here
# SECRET_KEY=your_secret_key_here
# DEBUG=false
`
}
