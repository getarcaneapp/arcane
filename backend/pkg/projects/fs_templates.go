package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

func ReadFolderComposeTemplate(baseDir, folder string) (string, *string, string, bool, error) {
	folderPath := filepath.Join(baseDir, folder)
	composePath, err := DetectComposeFile(folderPath)
	if err != nil {
		if errors.Is(err, common.ErrComposeFileNotFound) {
			return "", nil, "", false, nil
		}
		return "", nil, "", false, errors.WrapIf(err, "detect compose file")
	}

	b, err := os.ReadFile(composePath)
	if err != nil {
		return "", nil, "", false, errors.WrapIff(err, "read compose %s", composePath)
	}

	var envPtr *string
	for _, envName := range []string{".env.example", ".env"} {
		envPath := filepath.Join(folderPath, envName)
		if eb, rerr := os.ReadFile(envPath); rerr == nil {
			envPtr = new(string(eb))
			break
		}
	}

	desc := "Imported from " + composePath
	return string(b), envPtr, desc, true, nil
}

var (
	slugInvalidCharsPattern = regexp.MustCompile(`[^a-z0-9\-_]+`)
	slugDashRunPattern      = regexp.MustCompile(`-+`)
)

func Slugify(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	in = strings.ReplaceAll(in, " ", "-")
	in = slugInvalidCharsPattern.ReplaceAllString(in, "-")
	in = slugDashRunPattern.ReplaceAllString(in, "-")
	return strings.Trim(in, "-")
}

func EnsureTemplateDir(ctx context.Context, templatesDir, base string) (dir, composePath, envPath string, err error) {
	baseDir, derr := GetTemplatesDirectory(ctx, templatesDir)
	if derr != nil {
		return "", "", "", errors.WrapIf(derr, "ensure templates dir")
	}
	dir = filepath.Join(baseDir, base)
	if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil {
		return "", "", "", errors.WrapIf(err, "failed to create template directory")
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

func EnsureDefaultTemplates(ctx context.Context, configuredTemplatesDir string) error {
	templatesDir, err := GetTemplatesDirectory(ctx, configuredTemplatesDir)
	if err != nil {
		return errors.WrapIf(err, "get templates directory")
	}

	composePath := filepath.Join(templatesDir, ".compose.template")
	swarmStackPath := filepath.Join(templatesDir, ".swarm-stack.template")
	swarmStackEnvPath := filepath.Join(templatesDir, ".swarm-stack.env.template")
	envPath := filepath.Join(templatesDir, ".env.template")

	// Write default compose template if it doesn't exist
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		if err := WriteTemplateFile(composePath, getDefaultComposeTemplate()); err != nil {
			return errors.WrapIf(err, "write default compose template")
		}
	}

	// Write default swarm stack template if it doesn't exist
	if _, err := os.Stat(swarmStackPath); os.IsNotExist(err) {
		if err := WriteTemplateFile(swarmStackPath, DefaultSwarmStackTemplate()); err != nil {
			return errors.WrapIf(err, "write default swarm stack template")
		}
	}

	// Write default swarm stack env template if it doesn't exist
	if _, err := os.Stat(swarmStackEnvPath); os.IsNotExist(err) {
		if err := WriteTemplateFile(swarmStackEnvPath, DefaultSwarmStackEnvTemplate()); err != nil {
			return errors.WrapIf(err, "write default swarm stack env template")
		}
	}

	// Write default env template if it doesn't exist
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := WriteTemplateFile(envPath, getDefaultEnvTemplate()); err != nil {
			return errors.WrapIf(err, "write default env template")
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
      - .env
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

func DefaultSwarmStackTemplate() string {
	return `services:
  web:
    image: ${STACK_WEB_IMAGE:-nginx}:${STACK_WEB_TAG:-alpine}
    ports:
      - target: 80
        published: ${STACK_WEB_PUBLISHED_PORT:-8080}
        protocol: tcp
        mode: ingress
    deploy:
      mode: replicated
      replicas: ${STACK_WEB_REPLICAS:-2}
      update_config:
        parallelism: ${STACK_UPDATE_PARALLELISM:-1}
        delay: ${STACK_UPDATE_DELAY:-10s}
        order: start-first
      rollback_config:
        parallelism: ${STACK_ROLLBACK_PARALLELISM:-1}
        delay: ${STACK_ROLLBACK_DELAY:-5s}
        order: stop-first
      restart_policy:
        condition: on-failure
        delay: ${STACK_RESTART_DELAY:-5s}
    networks:
      - web

networks:
  web:
    driver: overlay
    name: ${STACK_OVERLAY_NETWORK:-web}
`
}

func DefaultSwarmStackEnvTemplate() string {
	return `# Docker Swarm stack variables
# These values are interpolated into the stack file before deployment.
# Example syntax in compose.yaml:
#   image: ${STACK_WEB_IMAGE:-nginx}:${STACK_WEB_TAG:-alpine}
#   replicas: ${STACK_WEB_REPLICAS:-2}

# Service image
STACK_WEB_IMAGE=nginx
STACK_WEB_TAG=alpine

# Published ingress port for the web service
STACK_WEB_PUBLISHED_PORT=8080

# Replica count for deploy.mode=replicated
STACK_WEB_REPLICAS=2

# Actual Docker overlay network name created for the stack
STACK_OVERLAY_NETWORK=web

# Rolling update behavior
STACK_UPDATE_PARALLELISM=1
STACK_UPDATE_DELAY=10s

# Rollback behavior
STACK_ROLLBACK_PARALLELISM=1
STACK_ROLLBACK_DELAY=5s

# Restart policy
STACK_RESTART_DELAY=5s
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
