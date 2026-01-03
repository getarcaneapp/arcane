package arcaneupdater

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// SelfUpdate handles the special case of updating the Arcane container itself
type SelfUpdate struct {
	dcli *client.Client
}

// NewSelfUpdate creates a new SelfUpdate handler
func NewSelfUpdate(dcli *client.Client) *SelfUpdate {
	return &SelfUpdate{dcli: dcli}
}

// IsArcaneContainerByID checks if a container ID belongs to an Arcane container
func (s *SelfUpdate) IsArcaneContainerByID(ctx context.Context, containerID string) (bool, error) {
	inspect, err := s.dcli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}

	if inspect.Config == nil {
		return false, nil
	}

	return IsArcaneContainer(inspect.Config.Labels), nil
}

// PrepareForSelfUpdate renames the current Arcane container so the new one can use its name
// Returns the new temporary name assigned to the old container
func (s *SelfUpdate) PrepareForSelfUpdate(ctx context.Context, containerID string, originalName string) (string, error) {
	// Generate a random temporary name
	tempName := generateTempName()

	slog.InfoContext(ctx, "PrepareForSelfUpdate: renaming Arcane container for self-update", "containerID", containerID, "originalName", originalName, "tempName", tempName)

	if err := s.dcli.ContainerRename(ctx, containerID, tempName); err != nil {
		return "", err
	}

	return tempName, nil
}

// CleanupOldArcaneInstances finds and removes old Arcane container instances
// This should be called on startup to clean up any leftover containers from previous updates
func (s *SelfUpdate) CleanupOldArcaneInstances(ctx context.Context, keepNewest bool) ([]string, error) {
	// Find all containers with the Arcane label
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", LabelArcane+"=true")

	containers, err := s.dcli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	// Only remove containers that were renamed by Arcane self-update.
	// This avoids deleting legitimate multi-instance Arcane containers.
	old := make([]container.Summary, 0, len(containers))
	for _, c := range containers {
		name := ExtractContainerName(c)
		if strings.HasPrefix(name, "arcane-old-") {
			old = append(old, c)
		}
	}

	if len(old) == 0 {
		return nil, nil // Nothing to clean up
	}

	// Sort by creation time (newest first)
	sortByCreated(old)

	var removed []string
	startIdx := 0
	if keepNewest {
		startIdx = 1 // Skip the newest container
	}

	for _, c := range old[startIdx:] {
		name := ExtractContainerName(c)

		slog.InfoContext(ctx, "CleanupOldArcaneInstances: removing old Arcane instance", "containerID", c.ID, "name", name, "created", c.Created)

		// Stop if running
		if c.State == "running" {
			timeout := 30 // seconds
			if err := s.dcli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
				slog.WarnContext(ctx, "CleanupOldArcaneInstances: failed to stop container", "containerID", c.ID, "error", err)
				continue
			}
		}

		// Remove container
		if err := s.dcli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			slog.WarnContext(ctx, "CleanupOldArcaneInstances: failed to remove container", "containerID", c.ID, "error", err)
			continue
		}

		removed = append(removed, c.ID)
	}

	return removed, nil
}

// GetCurrentArcaneContainer returns the currently running Arcane container, if any
func (s *SelfUpdate) GetCurrentArcaneContainer(ctx context.Context) (*container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", LabelArcane+"=true")
	filterArgs.Add("status", "running")

	containers, err := s.dcli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	if len(containers) == 0 {
		return nil, nil
	}

	// Return the newest running instance
	sortByCreated(containers)
	return &containers[0], nil
}

// sortByCreated sorts containers by creation time, newest first
func sortByCreated(containers []container.Summary) {
	for i := 0; i < len(containers)-1; i++ {
		for j := i + 1; j < len(containers); j++ {
			if containers[i].Created < containers[j].Created {
				containers[i], containers[j] = containers[j], containers[i]
			}
		}
	}
}

// generateTempName generates a random temporary name for the old container
func generateTempName() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely, but avoid failing the update due to entropy issues.
		return "arcane-old-fallback-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return "arcane-old-" + string(b)
}
