package libarcane

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"emperror.dev/errors"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
)

// InternalResourceLabel marks containers used for Arcane utilities, e.g. temp containers used for viewing volume files.
const InternalResourceLabel = "com.getarcaneapp.internal.resource"

func IsInternalContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, InternalResourceLabel) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "true", "1", "yes", "on":
				return true
			}
		}
	}
	return false
}

// LabelArcaneUpgrader marks the short-lived helper container that performs an
// Arcane self-upgrade. It carries the Arcane label too, so container lookups
// must skip it to avoid mistaking it for Arcane itself.
const LabelArcaneUpgrader = "com.getarcaneapp.arcane.upgrader"

// FindArcaneContainerIDByLabel locates Arcane's own container through the
// Arcane label. Running containers win; otherwise the first match is returned.
// The upgrader helper is never a candidate. Returns "" when nothing matches.
func FindArcaneContainerIDByLabel(ctx context.Context, dockerClient *client.Client) string {
	if dockerClient == nil {
		return ""
	}

	filters := make(client.Filters)
	filters = filters.Add("label", labels.LabelArcane+"=true")

	list, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		slog.DebugContext(ctx, "find arcane container by label: list failed", "error", err)
		return ""
	}

	var fallbackID string
	for _, c := range list.Items {
		if v, ok := c.Labels[LabelArcaneUpgrader]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
			continue
		}
		if c.State == container.StateRunning {
			return c.ID
		}
		if fallbackID == "" {
			fallbackID = c.ID
		}
	}

	slog.DebugContext(ctx, "find arcane container by label", "candidates", len(list.Items), "selected", fallbackID)
	return fallbackID
}

// InspectCurrentArcaneContainer inspects Arcane's own container, preferring
// cgroup/hostname self-detection and falling back to the Arcane label.
func InspectCurrentArcaneContainer(ctx context.Context, dockerClient *client.Client) (*container.InspectResponse, error) {
	if dockerClient == nil {
		return nil, errors.New("docker client is not available")
	}

	if target, err := CurrentContainerInspectTarget(cgroup.CurrentContainerID, os.Hostname); err == nil && target != "" {
		if inspect, inspectErr := ContainerInspectWithCompatibility(ctx, dockerClient, target, client.ContainerInspectOptions{}); inspectErr == nil {
			return &inspect.Container, nil
		}
	}

	containerID := FindArcaneContainerIDByLabel(ctx, dockerClient)
	if containerID == "" {
		return nil, errors.New("could not detect Arcane container")
	}

	inspect, err := ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, errors.WrapIf(err, "inspect Arcane container")
	}

	return &inspect.Container, nil
}

// CurrentContainerInspectTarget resolves the identifier used to inspect Arcane's current container.
func CurrentContainerInspectTarget(currentContainerID func() (string, error), hostname func() (string, error)) (string, error) {
	if currentContainerID != nil {
		if containerID, err := currentContainerID(); err == nil {
			if containerID = strings.TrimSpace(containerID); containerID != "" {
				return containerID, nil
			}
		}
	}

	if hostname == nil {
		hostname = os.Hostname
	}

	value, err := hostname()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(value), nil
}
