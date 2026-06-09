package selector

import (
	"context"
	"log/slog"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	libupdater "go.getarcane.app/updater/pkg/labels"
)

// UpgraderLabel marks detached helper containers launched for self-upgrades.
const UpgraderLabel = "com.getarcaneapp.arcane.upgrader"

// FindArcaneContainerByLabel returns the best Arcane container ID visible to the Docker daemon.
func FindArcaneContainerByLabel(ctx context.Context, dockerClient *client.Client) string {
	if dockerClient == nil {
		return ""
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		slog.DebugContext(ctx, "arcane container selector: failed to list containers", "error", err)
		return ""
	}

	containerSummary, ok := SelectArcaneContainerSummary(containerList.Items)
	if !ok {
		slog.DebugContext(ctx, "arcane container selector: no Arcane container candidate found", "count", len(containerList.Items))
		return ""
	}
	slog.DebugContext(
		ctx,
		"arcane container selector: selected Arcane container",
		"containerId",
		containerSummary.ID,
		"state",
		containerSummary.State,
		"count",
		len(containerList.Items),
	)
	return containerSummary.ID
}

// SelectArcaneContainerSummary chooses the best Arcane container summary from a Docker list.
func SelectArcaneContainerSummary(containerList []containertypes.Summary) (containertypes.Summary, bool) {
	var serverRunning, serverFallback, agentRunning, agentFallback *containertypes.Summary

	for i := range containerList {
		containerSummary := &containerList[i]
		labels := containerSummary.Labels
		if IsArcaneUpgrader(labels) {
			continue
		}

		running := containerSummary.State == containertypes.StateRunning
		switch {
		case libupdater.IsArcaneServerContainer(labels):
			if running && serverRunning == nil {
				serverRunning = containerSummary
			}
			if serverFallback == nil {
				serverFallback = containerSummary
			}
		case libupdater.IsArcaneAgentContainer(labels):
			if running && agentRunning == nil {
				agentRunning = containerSummary
			}
			if agentFallback == nil {
				agentFallback = containerSummary
			}
		}
	}

	for _, candidate := range []*containertypes.Summary{serverRunning, serverFallback, agentRunning, agentFallback} {
		if candidate != nil {
			return *candidate, true
		}
	}

	return containertypes.Summary{}, false
}

// IsArcaneUpgrader reports whether labels identify an Arcane self-upgrade helper.
func IsArcaneUpgrader(labels map[string]string) bool {
	for key, value := range labels {
		if strings.EqualFold(key, UpgraderLabel) {
			return strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	return false
}
