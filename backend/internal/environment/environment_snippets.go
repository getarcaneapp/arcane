package environment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
)

// DeploymentSnippets contains deployment configuration snippets for an environment.
type DeploymentSnippets struct {
	DockerRun     string
	DockerCompose string
	MTLS          *DeploymentSnippetMTLS
}

type DeploymentSnippetFile struct {
	Name          string `json:"name" doc:"Suggested filename"`
	Content       string `json:"content,omitempty" doc:"PEM file contents. Omitted for sensitive files such as private keys; use downloadUrl instead."`
	DownloadURL   string `json:"downloadUrl,omitempty" doc:"Pairing-permission endpoint to download this file when content is withheld"`
	Sensitive     bool   `json:"sensitive,omitempty" doc:"True when this file is sensitive and must be fetched via downloadUrl"`
	ContainerPath string `json:"containerPath" doc:"Container mount path expected by the mTLS snippet"`
	Permissions   string `json:"permissions" doc:"Suggested file mode"`
}

type DeploymentSnippetMTLS struct {
	DockerRun     string                  `json:"dockerRun" doc:"Docker run snippet using Arcane-generated mTLS assets"`
	DockerCompose string                  `json:"dockerCompose" doc:"Docker compose snippet using Arcane-generated mTLS assets"`
	Files         []DeploymentSnippetFile `json:"files" doc:"Generated PEM files to place on the edge host"`
	HostDirHint   string                  `json:"hostDirHint" doc:"Suggested host directory containing the generated PEM files"`
}

const (
	deploymentSnippetsDataPath = "/app/data"
	deploymentSnippetsMTLSPath = "/app/data/edge-mtls-agent"
)

// GenerateDeploymentSnippets generates Docker deployment snippets for an environment.
func (s *EnvironmentService) GenerateDeploymentSnippets(ctx context.Context, envID string, envAddress string, apiKey string) (*DeploymentSnippets, error) {
	managerURL := strings.TrimRight(envAddress, "/")

	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-agent \\",
		"  --restart unless-stopped \\",
		"  -e AGENT_MODE=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -p 3553:3553 \\",
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/agent:latest",
	}, "\n")

	dockerCompose := strings.Join([]string{
		"services:",
		"  arcane-agent:",
		"    image: ghcr.io/getarcaneapp/agent:latest",
		"    container_name: arcane-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - AGENT_MODE=true",
		"      - EDGE_TRANSPORT=poll",
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    ports:",
		"      - \"3553:3553\"",
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}, nil
}

// GenerateEdgeDeploymentSnippets generates Docker deployment snippets for an edge agent.
// Edge agents connect outbound to the manager and don't require exposed ports.
func (s *EnvironmentService) GenerateEdgeDeploymentSnippets(ctx context.Context, envID string, managerURL string, apiKey string, edgeCfg *edge.Config) (*DeploymentSnippets, error) {
	managerURL = strings.TrimRight(managerURL, "/")

	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/agent:latest",
	}, "\n")

	dockerCompose := strings.Join([]string{
		"# Edge agent - connects outbound, no exposed ports required",
		"services:",
		"  arcane-edge-agent:",
		"    image: ghcr.io/getarcaneapp/agent:latest",
		"    container_name: arcane-edge-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - EDGE_AGENT=true",
		"      - EDGE_TRANSPORT=poll",
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	snippets := &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}

	envName := ""
	if s != nil && s.db != nil {
		if env, getErr := s.GetEnvironmentByID(ctx, envID); getErr == nil && env != nil {
			envName = env.Name
		}
	}
	if edgeCfg != nil && strings.TrimSpace(edgeCfg.AppURL) == "" {
		edgeCfg.AppURL = managerURL
	}

	generatedAssets, err := edge.GenerateManagerClientMTLSAssetsWithContext(ctx, edgeCfg, envID, envName)
	if err != nil {
		slog.WarnContext(ctx, "Failed to generate edge mTLS assets; returning basic snippets only", "environment_id", envID, "error", err)
		return snippets, nil
	}
	if generatedAssets == nil {
		return snippets, nil
	}
	s.logGeneratedMTLSEventsInternal(ctx, envID, envName, generatedAssets)

	snippets.MTLS = buildMTLSDeploymentSnippetInternal(managerURL, apiKey, generatedAssets)
	return snippets, nil
}

func (s *EnvironmentService) logGeneratedMTLSEventsInternal(ctx context.Context, envID string, envName string, assets *edge.GeneratedMTLSAssets) {
	if s == nil || s.eventService == nil || assets == nil {
		return
	}
	if assets.CAGenerated {
		if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:        models.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    models.EventSeverityInfo,
			Title:       "Edge mTLS CA generated",
			Description: "Arcane generated a new edge mTLS certificate authority",
			Metadata:    models.JSON{"kind": "ca"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS CA generation event", "error", err)
		}
	}
	if assets.CertIssued {
		envIDCopy := envID
		if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSCertIssued,
			Severity:      models.EventSeverityInfo,
			Title:         "Edge mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an edge mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    &envIDCopy,
			ResourceName:  new(envName),
			EnvironmentID: &envIDCopy,
			Metadata:      models.JSON{"kind": "client"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS certificate issuance event", "environment_id", envID, "error", err)
		}
	}
}

func buildMTLSDeploymentSnippetInternal(managerURL string, apiKey string, generatedAssets *edge.GeneratedMTLSAssets) *DeploymentSnippetMTLS {
	if generatedAssets == nil {
		return nil
	}

	mtlsDockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		"  -e EDGE_MTLS_MODE=required \\",
		fmt.Sprintf("  -e EDGE_MTLS_ASSETS_DIR=%s \\", deploymentSnippetsMTLSPath),
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/agent:latest",
	}, "\n")

	mtlsDockerCompose := strings.Join([]string{
		"# Edge agent with automatic mTLS enrollment",
		"services:",
		"  arcane-edge-agent:",
		"    image: ghcr.io/getarcaneapp/agent:latest",
		"    container_name: arcane-edge-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - EDGE_AGENT=true",
		"      - EDGE_TRANSPORT=poll",
		"      - EDGE_MTLS_MODE=required",
		"      - EDGE_MTLS_ASSETS_DIR=" + deploymentSnippetsMTLSPath,
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	files := make([]DeploymentSnippetFile, 0, len(generatedAssets.Files))
	for _, file := range generatedAssets.Files {
		files = append(files, DeploymentSnippetFile{
			Name:          file.Name,
			Content:       file.Content,
			ContainerPath: file.ContainerPath,
			Permissions:   file.Permissions,
		})
	}

	return &DeploymentSnippetMTLS{
		DockerRun:     mtlsDockerRun,
		DockerCompose: mtlsDockerCompose,
		Files:         files,
		HostDirHint:   strings.TrimSpace(generatedAssets.HostDirHint),
	}
}
