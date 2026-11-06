package selfcontainer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/sys/cgroup"
	"kit.getarcane.app/updater/labels"
)

const (
	UpgradeLogDirectory   = "/app/data"
	InternalResourceLabel = "com.getarcaneapp.internal.resource"
	HiddenResourceLabel   = "com.getarcaneapp.arcane.hidden"
	LabelArcaneUpgrader   = "com.getarcaneapp.arcane.upgrader"
)

func FindArcaneContainerIDByLabel(ctx context.Context, dockerClient *client.Client) string {
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
		fallbackID = cmp.Or(fallbackID, c.ID)
	}

	slog.DebugContext(ctx, "find arcane container by label", "candidates", len(list.Items), "selected", fallbackID)
	return fallbackID
}

func InspectCurrentArcaneContainer(ctx context.Context, dockerClient *client.Client) (*container.InspectResponse, error) {
	// Behind a network_mode sidecar, hostname and cgroup resolve the sidecar, so only a labeled hit is trusted (#3544, #3693).
	var unlabeled *container.InspectResponse
	if target, err := CurrentContainerInspectTarget(cgroup.CurrentContainerID, os.Hostname); err == nil && target != "" {
		if inspect, inspectErr := compat.ContainerInspectWithCompatibility(ctx, dockerClient, target, client.ContainerInspectOptions{}); inspectErr == nil {
			if inspect.Container.Config != nil && strings.EqualFold(strings.TrimSpace(inspect.Container.Config.Labels[labels.LabelArcane]), "true") {
				return &inspect.Container, nil
			}
			unlabeled = &inspect.Container
		}
	}

	if containerID := FindArcaneContainerIDByLabel(ctx, dockerClient); containerID != "" {
		inspect, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("inspect Arcane container: %w", err)
		}
		// A labeled match overrides an unlabeled hit only when it shares that hit's network namespace.
		if unlabeled == nil || sharesNetworkNamespace(&inspect.Container, unlabeled) {
			return &inspect.Container, nil
		}
	}

	if unlabeled != nil {
		return unlabeled, nil
	}

	return nil, errors.New("could not detect Arcane container")
}

func sharesNetworkNamespace(candidate, owner *container.InspectResponse) bool {
	if candidate.HostConfig == nil || !candidate.HostConfig.NetworkMode.IsContainer() {
		return false
	}
	ref := strings.TrimSpace(candidate.HostConfig.NetworkMode.ConnectedContainer())
	if ref == "" {
		return false
	}
	return strings.HasPrefix(owner.ID, ref) || strings.EqualFold(ref, strings.TrimPrefix(owner.Name, "/"))
}

func CurrentContainerInspectTarget(currentContainerID, hostname func() (string, error)) (string, error) {
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
