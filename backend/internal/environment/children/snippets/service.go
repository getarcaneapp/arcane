// Package snippets renders agent deployment snippets and issues the edge mTLS
// assets they reference.
package snippets

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
)

const (
	DeploymentSnippetsDataPath = "/app/data"
	DeploymentSnippetsMTLSPath = "/app/data/edge-mtls-agent"
	AgentContainerPort         = "3553"
)

// Service issues edge mTLS assets and records their audit events.
type Service struct {
	eventService *event.EventService
}

func New(eventService *event.EventService) *Service {
	return &Service{eventService: eventService}
}

// Direct renders docker run and compose snippets for a direct agent.
func Direct(managerURL, agentURL, apiKey string) (string, string) {
	portMapping := fmt.Sprintf("%s:%s", AgentHostPort(agentURL), AgentContainerPort)

	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-agent \\",
		"  --restart unless-stopped \\",
		"  -e AGENT_MODE=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		fmt.Sprintf("  -p %s \\", portMapping),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", DeploymentSnippetsDataPath),
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
		fmt.Sprintf("      - %q", portMapping),
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + DeploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return dockerRun, dockerCompose
}

// Edge renders docker run and compose snippets for an edge agent, which
// connects outbound and needs no exposed ports.
func Edge(managerURL, apiKey string) (string, string) {
	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", DeploymentSnippetsDataPath),
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
		"      - arcane-data:" + DeploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return dockerRun, dockerCompose
}

// GenerateMTLS issues manager-signed edge mTLS assets and renders the snippets
// that enroll with them. Nil assets mean only the basic snippets apply.
func (s *Service) GenerateMTLS(ctx context.Context, edgeCfg *edge.Config, envID, envName, managerURL, apiKey string) (*edge.GeneratedMTLSAssets, string, string) {
	generatedAssets, err := edge.GenerateManagerClientMTLSAssetsWithContext(ctx, edgeCfg, envID, envName)
	if err != nil {
		slog.WarnContext(ctx, "Failed to generate edge mTLS assets; returning basic snippets only", "environmentId", envID, "error", err)
		return nil, "", ""
	}
	if generatedAssets == nil {
		return nil, "", ""
	}
	s.logGeneratedMTLSEventsInternal(ctx, envID, envName, generatedAssets)

	mtlsDockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		"  -e EDGE_MTLS_MODE=required \\",
		fmt.Sprintf("  -e EDGE_MTLS_ASSETS_DIR=%s \\", DeploymentSnippetsMTLSPath),
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", DeploymentSnippetsDataPath),
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
		"      - EDGE_MTLS_ASSETS_DIR=" + DeploymentSnippetsMTLSPath,
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + DeploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return generatedAssets, mtlsDockerRun, mtlsDockerCompose
}

func (s *Service) logGeneratedMTLSEventsInternal(ctx context.Context, envID, envName string, assets *edge.GeneratedMTLSAssets) {
	if s == nil || s.eventService == nil || assets == nil {
		return
	}
	if assets.CAGenerated {
		if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:        event.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    event.EventSeverityInfo,
			Title:       "Edge mTLS CA generated",
			Description: "Arcane generated a new edge mTLS certificate authority",
			Metadata:    database.JSON{"kind": "ca"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS CA generation event", "error", err)
		}
	}
	if assets.CertIssued {
		envIDCopy := envID
		if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeEnvironmentMTLSCertIssued,
			Severity:      event.EventSeverityInfo,
			Title:         "Edge mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an edge mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    &envIDCopy,
			ResourceName:  new(envName),
			EnvironmentID: &envIDCopy,
			Metadata:      database.JSON{"kind": "client"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS certificate issuance event", "environmentId", envID, "error", err)
		}
	}
}

func AgentHostPort(agentURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(agentURL))
	if err != nil || parsed.Host == "" {
		return AgentContainerPort
	}
	if port := parsed.Port(); port != "" {
		return port
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return AgentContainerPort
	}
}
