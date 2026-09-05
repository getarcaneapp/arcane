package projects

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"emperror.dev/errors"
	composeloader "github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
)

// ReadFolderComposeTemplate stays on os.* for its reads: template folders are
// user-managed on disk, so compose/env files may be symlinks resolving outside
// any confinement root, which acfs cannot follow.
func ReadFolderComposeTemplate(ctx context.Context, baseDir, folder string) (string, *string, string, bool, error) {
	folderPath := filepath.Join(baseDir, folder)
	composePath, err := DetectComposeFile(ctx, "", folderPath)
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
	dirLogical, err := acfs.LogicalPath(baseDir, dir)
	if err != nil {
		return "", "", "", errors.WrapIf(err, "template directory is outside the templates root")
	}
	if err := acfs.MkdirAll(ctx, baseDir, dirLogical, utils.DirPerm); err != nil {
		return "", "", "", errors.WrapIf(err, "failed to create template directory")
	}
	composePath = filepath.Join(dir, "compose.yaml")
	envPath = filepath.Join(dir, ".env.example")
	return dir, composePath, envPath, nil
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

	// Write default compose template if it doesn't exist
	if exists, err := acfs.Exists(ctx, templatesDir, "/.compose.template"); err != nil {
		return errors.WrapIf(err, "write default compose template")
	} else if !exists {
		if err := acfs.Write(ctx, templatesDir, "/.compose.template", []byte(getDefaultComposeTemplate()), acfs.WriteOptions{Mode: utils.FilePerm}); err != nil {
			return errors.WrapIf(err, "write default compose template")
		}
	}

	// Write default swarm stack template if it doesn't exist
	if exists, err := acfs.Exists(ctx, templatesDir, "/.swarm-stack.template"); err != nil {
		return errors.WrapIf(err, "write default swarm stack template")
	} else if !exists {
		if err := acfs.Write(ctx, templatesDir, "/.swarm-stack.template", []byte(DefaultSwarmStackTemplate()), acfs.WriteOptions{Mode: utils.FilePerm}); err != nil {
			return errors.WrapIf(err, "write default swarm stack template")
		}
	}

	// Write default swarm stack env template if it doesn't exist
	if exists, err := acfs.Exists(ctx, templatesDir, "/.swarm-stack.env.template"); err != nil {
		return errors.WrapIf(err, "write default swarm stack env template")
	} else if !exists {
		if err := acfs.Write(ctx, templatesDir, "/.swarm-stack.env.template", []byte(DefaultSwarmStackEnvTemplate()), acfs.WriteOptions{Mode: utils.FilePerm}); err != nil {
			return errors.WrapIf(err, "write default swarm stack env template")
		}
	}

	// Write default env template if it doesn't exist
	if exists, err := acfs.Exists(ctx, templatesDir, "/.env.template"); err != nil {
		return errors.WrapIf(err, "write default env template")
	} else if !exists {
		if err := acfs.Write(ctx, templatesDir, "/.env.template", []byte(getDefaultEnvTemplate()), acfs.WriteOptions{Mode: utils.FilePerm}); err != nil {
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

// ParseComposeServices extracts service names from a compose file content using compose-go
func ParseComposeServices(ctx context.Context, composeContent string) []string {
	if composeContent == "" {
		return []string{}
	}

	// Create a temp directory with dummy .env file to satisfy env_file references
	// System temp scratch: no acfs root exists for it.
	tmpDir, err := os.MkdirTemp("", "arcane-compose-parse-*")
	if err != nil {
		slog.WarnContext(ctx, "Failed to create temp dir for compose parsing", "error", err)
		return []string{}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a dummy .env file to prevent env file errors
	envPath := filepath.Join(tmpDir, ".env")
	if err := WriteFileWithPerm(envPath, "", utils.FilePerm); err != nil {
		slog.WarnContext(ctx, "Failed to create dummy env file", "error", err)
	}

	// Parse using compose-go
	configDetails := composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{
			{
				Content: []byte(composeContent),
			},
		},
		WorkingDir:  tmpDir,
		Environment: composetypes.Mapping{},
	}

	project, err := composeloader.LoadWithContext(ctx, configDetails, composeloader.WithSkipValidation)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse compose services", "error", err)
		return []string{}
	}

	serviceNames := make([]string, 0, len(project.Services))
	for _, service := range project.Services {
		serviceNames = append(serviceNames, service.Name)
	}

	return serviceNames
}

// ResolveTemplateIconURL reads Arcane icon metadata from template Compose content.
func ResolveTemplateIconURL(ctx context.Context, composeContent, envContent string) *string {
	if strings.TrimSpace(composeContent) == "" {
		return nil
	}

	// System temp scratch: no acfs root exists for it.
	tmpDir, err := os.MkdirTemp("", "arcane-template-icon-*")
	if err != nil {
		slog.WarnContext(ctx, "failed to create temp dir for template icon parsing", "error", err)
		return nil
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	envPath := filepath.Join(tmpDir, ".env")
	if err := WriteFileWithPerm(envPath, envContent, utils.FilePerm); err != nil {
		slog.WarnContext(ctx, "failed to create temp env file for template icon parsing", "error", err)
	}

	envMap := make(composetypes.Mapping)
	for _, variable := range ParseEnvContent(envContent) {
		if key := strings.TrimSpace(variable.Key); key != "" {
			envMap[key] = variable.Value
		}
	}
	envMap["PWD"] = tmpDir

	configDetails := composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{
			{
				Content: []byte(composeContent),
			},
		},
		WorkingDir:  tmpDir,
		Environment: envMap,
	}

	project, err := composeloader.LoadWithContext(ctx, configDetails, composeloader.WithSkipValidation, func(opts *composeloader.Options) {
		opts.SkipConsistencyCheck = true
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to parse compose for template icon metadata", "error", err)
		return nil
	}

	if project == nil {
		return nil
	}

	icon, _, _, _ := parseArcaneBlockInternal(project.Extensions[arcaneBlockKey])
	return mo.EmptyableToOption(strings.TrimSpace(icon.Icon)).ToPointer()
}
