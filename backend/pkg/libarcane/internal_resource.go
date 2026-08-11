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

	// A cgroup-derived container ID is definitively the current container, so
	// that inspect is trusted even without the Arcane label (custom images).
	// With network_mode: service:<sidecar> the hostname belongs to the sidecar
	// container, so a hostname-derived inspect is only trusted directly when it
	// carries the Arcane label (#3544); an unlabeled hostname hit is kept as a
	// last resort so the label lookup can't hijack detection either way.
	var unlabeled *container.InspectResponse
	if target, fromContainerID, err := CurrentContainerInspectTarget(cgroup.CurrentContainerID, os.Hostname); err == nil && target != "" {
		if inspect, inspectErr := ContainerInspectWithCompatibility(ctx, dockerClient, target, client.ContainerInspectOptions{}); inspectErr == nil {
			if fromContainerID {
				return &inspect.Container, nil
			}
			if inspect.Container.Config != nil && strings.EqualFold(strings.TrimSpace(inspect.Container.Config.Labels[labels.LabelArcane]), "true") {
				return &inspect.Container, nil
			}
			unlabeled = &inspect.Container
		}
	}

	if containerID := FindArcaneContainerIDByLabel(ctx, dockerClient); containerID != "" {
		inspect, err := ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, errors.WrapIf(err, "inspect Arcane container")
		}
		// An unlabeled hostname hit is only overridden by the labeled match when
		// that match shares the hostname container's network namespace — i.e. the
		// hostname really belonged to its sidecar (#3544). Otherwise the labeled
		// container is a different Arcane instance and the hostname hit is us.
		if unlabeled == nil || sharesNetworkNamespaceInternal(&inspect.Container, unlabeled) {
			return &inspect.Container, nil
		}
	}

	if unlabeled != nil {
		return unlabeled, nil
	}

	return nil, errors.New("could not detect Arcane container")
}

// sharesNetworkNamespaceInternal reports whether candidate runs with
// network_mode "container:<owner>", i.e. it borrows owner's network namespace
// (and therefore owner's hostname). The reference may be the owner's ID (any
// prefix length) or its name.
func sharesNetworkNamespaceInternal(candidate, owner *container.InspectResponse) bool {
	if candidate.HostConfig == nil || !candidate.HostConfig.NetworkMode.IsContainer() {
		return false
	}
	ref := strings.TrimSpace(candidate.HostConfig.NetworkMode.ConnectedContainer())
	if ref == "" {
		return false
	}
	return strings.HasPrefix(owner.ID, ref) || strings.EqualFold(ref, strings.TrimPrefix(owner.Name, "/"))
}

// CurrentContainerInspectTarget resolves the identifier used to inspect Arcane's
// current container. The second return reports whether the target came from the
// cgroup container ID (authoritative) rather than the hostname fallback.
func CurrentContainerInspectTarget(currentContainerID func() (string, error), hostname func() (string, error)) (string, bool, error) {
	if currentContainerID != nil {
		if containerID, err := currentContainerID(); err == nil {
			if containerID = strings.TrimSpace(containerID); containerID != "" {
				return containerID, true, nil
			}
		}
	}

	if hostname == nil {
		hostname = os.Hostname
	}

	value, err := hostname()
	if err != nil {
		return "", false, err
	}

	return strings.TrimSpace(value), false, nil
}
