// Package snippets renders agent deployment snippets and issues the agent mTLS
// assets they reference.
package snippets

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
)

const (
	DeploymentSnippetsDataPath = "/app/data"
	DeploymentSnippetsMTLSPath = "/app/data/agent-mtls-client"
)

// Service issues agent mTLS assets and records their audit events.
type Service struct {
	eventService *event.EventService
}

func New(eventService *event.EventService) *Service {
	return &Service{eventService: eventService}
}

// Agent renders docker run and compose snippets for an agent, which
// connects outbound and needs no exposed ports.
func Agent(managerURL, apiKey string) (string, string) {
	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-agent \\",
		"  --restart unless-stopped \\",
		"  -e ARCANE_AGENT=true \\",
		"  -e ARCANE_AGENT_TRANSPORT=auto \\",
		fmt.Sprintf("  -e ARCANE_AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e ARCANE_MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", DeploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/agent:latest",
	}, "\n")

	dockerCompose := strings.Join([]string{
		"# Agent - connects outbound, no exposed ports required",
		"services:",
		"  arcane-agent:",
		"    image: ghcr.io/getarcaneapp/agent:latest",
		"    container_name: arcane-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - ARCANE_AGENT=true",
		"      - ARCANE_AGENT_TRANSPORT=auto",
		"      - ARCANE_AGENT_TOKEN=" + apiKey,
		"      - ARCANE_MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + DeploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return dockerRun, dockerCompose
}

// GenerateMTLS issues manager-signed agent mTLS assets and renders the snippets
// that enroll with them. Nil assets mean only the basic snippets apply.
func (s *Service) GenerateMTLS(ctx context.Context, agentCfg *agent.Config, envID, envName, managerURL, apiKey string) (*agent.GeneratedMTLSAssets, string, string) {
	generatedAssets, err := agent.GenerateManagerClientMTLSAssetsWithContext(ctx, agentCfg, envID, envName)
	if err != nil {
		slog.WarnContext(ctx, "Failed to generate agent mTLS assets; returning basic snippets only", "environmentId", envID, "error", err)
		return nil, "", ""
	}
	if generatedAssets == nil {
		return nil, "", ""
	}
	if generatedAssets.CAGenerated {
		if _, caEventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:        event.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    event.EventSeverityInfo,
			Title:       "Agent mTLS CA generated",
			Description: "Arcane generated a new agent mTLS certificate authority",
			Metadata:    database.JSON{"kind": "ca"},
		}); caEventErr != nil {
			slog.WarnContext(ctx, "Failed to create agent mTLS CA generation event", "error", caEventErr)
		}
	}
	if generatedAssets.CertIssued {
		if _, certEventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeEnvironmentMTLSCertIssued,
			Severity:      event.EventSeverityInfo,
			Title:         "Agent mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an agent mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    new(envID),
			ResourceName:  new(envName),
			EnvironmentID: new(envID),
			Metadata:      database.JSON{"kind": "client"},
		}); certEventErr != nil {
			slog.WarnContext(ctx, "Failed to create agent mTLS certificate issuance event", "environmentId", envID, "error", certEventErr)
		}
	}

	mtlsDockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-agent \\",
		"  --restart unless-stopped \\",
		"  -e ARCANE_AGENT=true \\",
		"  -e ARCANE_AGENT_TRANSPORT=auto \\",
		"  -e ARCANE_AGENT_MTLS_MODE=required \\",
		fmt.Sprintf("  -e ARCANE_AGENT_MTLS_ASSETS_DIR=%s \\", DeploymentSnippetsMTLSPath),
		fmt.Sprintf("  -e ARCANE_AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e ARCANE_MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", DeploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/agent:latest",
	}, "\n")

	mtlsDockerCompose := strings.Join([]string{
		"# Agent with automatic mTLS enrollment",
		"services:",
		"  arcane-agent:",
		"    image: ghcr.io/getarcaneapp/agent:latest",
		"    container_name: arcane-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - ARCANE_AGENT=true",
		"      - ARCANE_AGENT_TRANSPORT=auto",
		"      - ARCANE_AGENT_MTLS_MODE=required",
		"      - ARCANE_AGENT_MTLS_ASSETS_DIR=" + DeploymentSnippetsMTLSPath,
		"      - ARCANE_AGENT_TOKEN=" + apiKey,
		"      - ARCANE_MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + DeploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return generatedAssets, mtlsDockerRun, mtlsDockerCompose
}
