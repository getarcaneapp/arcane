package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/arcaneupdater"
	arcRegistry "github.com/getarcaneapp/arcane/backend/internal/utils/registry"
	"github.com/getarcaneapp/arcane/types/updater"
)

type UpdaterService struct {
	db                  *database.DB
	settingsService     *SettingsService
	dockerService       *DockerClientService
	projectService      *ProjectService
	imageUpdateService  *ImageUpdateService
	registryService     *ContainerRegistryService
	eventService        *EventService
	imageService        *ImageService
	notificationService *NotificationService
	upgradeService      *SystemUpgradeService

	updatingContainers map[string]bool
	updatingProjects   map[string]bool
}

func NewUpdaterService(
	db *database.DB,
	settings *SettingsService,
	docker *DockerClientService,
	projects *ProjectService,
	imageUpdates *ImageUpdateService,
	registries *ContainerRegistryService,
	events *EventService,
	imageSvc *ImageService,
	notifications *NotificationService,
	upgrade *SystemUpgradeService,
) *UpdaterService {
	return &UpdaterService{
		db:                  db,
		settingsService:     settings,
		dockerService:       docker,
		projectService:      projects,
		imageUpdateService:  imageUpdates,
		registryService:     registries,
		eventService:        events,
		imageService:        imageSvc,
		notificationService: notifications,
		upgradeService:      upgrade,
		updatingContainers:  map[string]bool{},
		updatingProjects:    map[string]bool{},
	}
}

//nolint:gocognit
func (s *UpdaterService) ApplyPending(ctx context.Context, dryRun bool) (*updater.Result, error) {
	start := time.Now()
	out := &updater.Result{Items: []updater.ResourceResult{}}

	var records []models.ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("has_update = ?", true).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query pending image updates: %w", err)
	}
	// debug: how many pending records and dryRun flag
	slog.DebugContext(ctx, "ApplyPending: found pending image update records", "records", len(records), "dryRun", dryRun)

	if len(records) == 0 {
		out.Duration = time.Since(start).String()
		return out, nil
	}

	// Only update images that are actually used by running resources
	usedImages, err := s.collectUsedImages(ctx)
	if err != nil {
		// Non-fatal: continue without the filter
		usedImages = map[string]struct{}{}
	}

	// Plan updates and capture OLD image digests before pull
	type updatePlan struct {
		oldRef string
		newRef string
		oldIDs []string // sha256:... image IDs that currently back oldRef
		pulled bool
	}
	var plans []updatePlan

	for _, r := range records {
		if r.Repository == "" || r.Tag == "" {
			continue
		}
		oldRef := fmt.Sprintf("%s:%s", r.Repository, r.Tag)
		oldNorm := s.normalizeRef(oldRef)

		if len(usedImages) > 0 {
			if _, ok := usedImages[oldNorm]; !ok {
				continue
			}
		}

		newRef := oldRef
		if r.IsTagUpdate() && r.LatestVersion != nil && *r.LatestVersion != "" {
			newRef = fmt.Sprintf("%s:%s", r.Repository, *r.LatestVersion)
		}

		oldIDs, _ := s.resolveLocalImageIDsForRef(ctx, oldRef)
		plans = append(plans, updatePlan{oldRef: oldRef, newRef: newRef, oldIDs: oldIDs})
	}

	if len(plans) == 0 {
		out.Duration = time.Since(start).String()
		return out, nil
	}

	// Log run start
	s.logAutoUpdate(ctx, models.EventSeverityInfo, models.JSON{
		"phase":   "start",
		"dryRun":  dryRun,
		"planned": len(plans),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})

	// Pull images with ImageService (waits for completion)
	// Only containers using the OLD image IDs will be restarted after pulls succeed.
	// This prevents restarts when pulls fail or when the image digest didn't change.
	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("docker connect: %w", err)
	}
	registryClient := arcRegistry.NewClient()
	digestChecker := arcaneupdater.NewDigestChecker(dcli, registryClient)

	enabledRegs := []models.ContainerRegistry{}
	if s.registryService != nil {
		if regs, rerr := s.registryService.GetEnabledRegistries(ctx); rerr == nil {
			enabledRegs = regs
		}
	}

	// track all old image IDs we saw for pulled updates so we can prune them after restart
	oldIDSet := map[string]struct{}{}

	for i := range plans {
		p := plans[i]
		item := updater.ResourceResult{
			ResourceID:   p.oldRef,
			ResourceType: "image",
			ResourceName: p.oldRef,
			Status:       "checked",
			OldImages:    map[string]string{"main": p.oldRef},
			NewImages:    map[string]string{"main": p.newRef},
		}
		out.Checked++

		if dryRun {
			item.Status = "skipped"
			out.Skipped++
			out.Items = append(out.Items, item)
			_ = s.recordRun(ctx, item)

			s.logAutoUpdate(ctx, s.severityFromStatus(item.Status), models.JSON{
				"phase":    "image_pull",
				"imageOld": p.oldRef,
				"imageNew": p.newRef,
				"status":   item.Status,
				"dryRun":   true,
			})
			continue
		}

		// Digest pre-check: if registry supports it and digests match, avoid pulling entirely.
		// This also prevents unnecessary restarts when the update record is stale.
		normNew := s.normalizeRef(p.newRef)
		host, repo, tag := s.parseNormalizedRef(normNew)
		authHeader, _, _, _ := arcRegistry.ResolveAuthHeaderForRepository(ctx, host, repo, tag, enabledRegs)
		check := digestChecker.CheckImageNeedsUpdate(ctx, normNew, authHeader)
		skipPull := false

		if check.CheckedViaAPI && check.Error == nil && !check.NeedsUpdate {
			item.Status = "skipped"
			item.Error = "image already up to date"
			out.Skipped++
			// We skip checking for pull, but we still proceed to container update checks
			// treating this as "successful" for the pipeline, but invalidating oldIDs
			// because they represent the *current* image, not a stale one.
			plans[i].pulled = true
			plans[i].oldIDs = nil
			skipPull = true

			s.logAutoUpdate(ctx, s.severityFromStatus(item.Status), models.JSON{
				"phase":         "image_pull",
				"imageOld":      p.oldRef,
				"imageNew":      p.newRef,
				"status":        item.Status,
				"digestLocal":   check.LocalDigest,
				"digestRemote":  check.RemoteDigest,
				"checkedViaApi": true,
				"error":         item.Error,
			})
		}

		if !skipPull {
			if err := s.imageService.PullImage(ctx, p.newRef, io.Discard, systemUser, nil); err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				out.Failed++
			} else {
				item.Status = "updated"
				item.UpdateApplied = true
				out.Updated++
				plans[i].pulled = true
				for _, id := range p.oldIDs {
					if id != "" {
						oldIDSet[id] = struct{}{}
					}
				}
			}
			s.logAutoUpdate(ctx, s.severityFromStatus(item.Status), models.JSON{
				"phase":    "image_pull",
				"imageOld": p.oldRef,
				"imageNew": p.newRef,
				"status":   item.Status,
				"error":    item.Error,
			})
		}

		out.Items = append(out.Items, item)
		_ = s.recordRun(ctx, item)
	}

	// Build maps for fast matching later (only for successfully pulled updates)
	oldRefToNewRef := map[string]string{}
	oldIDToNewRef := map[string]string{} // sha256 -> newRef
	for _, p := range plans {
		if !p.pulled {
			continue
		}
		oldRefToNewRef[p.oldRef] = p.newRef
		for _, id := range p.oldIDs {
			if id != "" {
				oldIDToNewRef[id] = p.newRef
			}
		}
	}

	if !dryRun && (len(oldIDToNewRef) > 0 || len(oldRefToNewRef) > 0) {
		results, err := s.restartContainersUsingOldIDs(ctx, oldIDToNewRef, oldRefToNewRef)
		if err != nil {
			slog.Warn("container restarts had errors", "err", err)
		}
		for _, r := range results {
			item := updater.ResourceResult{
				ResourceID:    r.ResourceID,
				ResourceType:  "container",
				ResourceName:  r.ResourceName,
				Status:        r.Status,
				Error:         r.Error,
				OldImages:     r.OldImages,
				NewImages:     r.NewImages,
				UpdateApplied: r.UpdateApplied,
			}
			out.Items = append(out.Items, item)
			out.Checked++
			switch {
			case r.UpdateApplied:
				out.Updated++
			case r.Error != "":
				out.Failed++
			default:
				out.Skipped++
			}
			_ = s.recordRun(ctx, item)

			s.logAutoUpdate(ctx, s.severityFromStatus(item.Status), models.JSON{
				"phase":        "container",
				"containerId":  r.ResourceID,
				"container":    r.ResourceName,
				"status":       r.Status,
				"oldImageMain": r.OldImages["main"],
				"newImageMain": r.NewImages["main"],
				"error":        r.Error,
			})
		}
	}

	// Prune old images that are no longer used (only for images that were actually updated)
	if !dryRun && len(oldIDSet) > 0 {
		ids := make([]string, 0, len(oldIDSet))
		for id := range oldIDSet {
			ids = append(ids, id)
		}
		if err := s.pruneImageIDs(ctx, ids); err != nil {
			slog.Warn("image prune failed", "err", err)
		}
	}

	// After applying updates, clear has_update locally if no containers still use old image IDs.
	if !dryRun {
		for _, p := range plans {
			if len(p.oldIDs) == 0 {
				continue
			}
			stillUsed, _ := s.anyImageIDsStillInUse(ctx, p.oldIDs)
			if stillUsed {
				continue
			}
			repo, tag := s.parseRepoAndTag(p.oldRef)
			if repo == "" || tag == "" {
				continue
			}
			if err := s.clearImageUpdateRecord(ctx, repo, tag); err == nil {
				s.logAutoUpdate(ctx, models.EventSeverityInfo, models.JSON{
					"phase":    "record_clear",
					"imageOld": p.oldRef,
					"status":   "cleared",
				})
			}
		}

		if err := s.imageUpdateService.CleanupOrphanedRecords(ctx); err != nil {
			slog.Warn("cleanup orphaned update records failed", "err", err)
		}
	}

	// Log run complete
	duration := time.Since(start).String()
	out.Duration = duration
	s.logAutoUpdate(ctx, models.EventSeverityInfo, models.JSON{
		"phase":    "complete",
		"checked":  out.Checked,
		"updated":  out.Updated,
		"skipped":  out.Skipped,
		"failed":   out.Failed,
		"duration": duration,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})

	return out, nil
}

// UpdateSingleContainer updates a single container by ID to the latest available image.
// It pulls the new image, stops the container, removes it, and recreates it with the new image.
func (s *UpdaterService) UpdateSingleContainer(ctx context.Context, containerID string) (*updater.Result, error) {
	start := time.Now()
	out := &updater.Result{Items: []updater.ResourceResult{}}

	slog.InfoContext(ctx, "UpdateSingleContainer: starting", "containerID", containerID)

	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("docker connect: %w", err)
	}

	// Get container info
	containers, err := dcli.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(filters.Arg("id", containerID))})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var targetContainer *container.Summary
	if len(containers) > 0 {
		targetContainer = &containers[0]
	}

	if targetContainer == nil {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	containerName := s.getContainerName(*targetContainer)
	slog.InfoContext(ctx, "UpdateSingleContainer: found container", "containerID", containerID, "name", containerName, "image", targetContainer.Image)

	// Inspect container to get full config (needed for label-based controls)
	inspectBefore, err := dcli.ContainerInspect(ctx, targetContainer.ID)
	if err != nil {
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "failed",
			Error:        fmt.Sprintf("inspect failed: %v", err),
		})
		out.Failed++
		out.Duration = time.Since(start).String()
		return out, nil
	}

	labels := map[string]string{}
	if inspectBefore.Config != nil && inspectBefore.Config.Labels != nil {
		labels = inspectBefore.Config.Labels
	}

	isArcaneContainer := arcaneupdater.IsArcaneContainer(labels)
	slog.InfoContext(ctx, "UpdateSingleContainer: inspected container",
		"containerID", containerID,
		"imageID", inspectBefore.Image,
		"isArcane", isArcaneContainer,
		"hasArcaneLabel", labels["com.getarcaneapp.arcane"])

	if arcaneupdater.IsUpdateDisabled(labels) {
		slog.InfoContext(ctx, "UpdateSingleContainer: updates disabled by label", "containerID", containerID)
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "skipped",
			Error:        "updates disabled by label",
		})
		out.Skipped++
		out.Checked = 1
		out.Duration = time.Since(start).String()
		return out, nil
	}

	// Get the image reference
	imageRef := targetContainer.Image
	normalizedRef := s.normalizeRef(imageRef)
	repo, tag := s.parseRepoAndTag(normalizedRef)

	if repo == "" || tag == "" {
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "skipped",
			Error:        "invalid image reference",
		})
		out.Skipped++
		out.Duration = time.Since(start).String()
		return out, nil
	}

	slog.InfoContext(ctx, "UpdateSingleContainer: pulling new image", "containerID", containerID, "image", normalizedRef)

	// Pull the latest image using the image service
	if err := s.imageService.PullImage(ctx, normalizedRef, io.Discard, systemUser, nil); err != nil {
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "failed",
			Error:        fmt.Sprintf("pull failed: %v", err),
		})
		out.Failed++
		out.Duration = time.Since(start).String()
		return out, nil
	}

	// Compare with pulled image to avoid unnecessary restart
	checker := arcaneupdater.NewDigestChecker(dcli, arcRegistry.NewClient())
	changed, cmpErr := checker.CompareWithPulled(ctx, inspectBefore.Image, normalizedRef)
	slog.InfoContext(ctx, "UpdateSingleContainer: digest comparison",
		"containerID", containerID,
		"changed", changed,
		"compareError", cmpErr,
		"oldImageID", inspectBefore.Image,
		"normalizedRef", normalizedRef)

	if cmpErr == nil && !changed {
		slog.InfoContext(ctx, "UpdateSingleContainer: no update needed - digest unchanged",
			"containerID", containerID,
			"imageID", inspectBefore.Image)
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "skipped",
			Error:        "image digest unchanged after pull",
		})
		out.Skipped++
		out.Checked = 1
		out.Duration = time.Since(start).String()
		return out, nil
	}

	inspect := inspectBefore

	// Check if this is Arcane self-update - use CLI upgrade instead
	containerLabels := map[string]string{}
	if inspect.Config != nil && inspect.Config.Labels != nil {
		containerLabels = inspect.Config.Labels
	}

	if arcaneupdater.IsArcaneContainer(containerLabels) && s.upgradeService != nil {
		slog.InfoContext(ctx, "UpdateSingleContainer: detected Arcane self-update, using CLI upgrade method", "containerID", containerID)

		if err := s.upgradeService.TriggerUpgradeViaCLI(ctx, systemUser); err != nil {
			out.Items = append(out.Items, updater.ResourceResult{
				ResourceID:   targetContainer.ID,
				ResourceType: "container",
				ResourceName: containerName,
				Status:       "failed",
				Error:        fmt.Sprintf("CLI upgrade failed: %v", err),
			})
			out.Failed++
			out.Duration = time.Since(start).String()
			return out, nil
		}

		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "updated",
		})
		out.Updated++
		out.Checked = 1
		out.Duration = time.Since(start).String()

		slog.InfoContext(ctx, "UpdateSingleContainer: CLI upgrade triggered successfully", "containerID", containerID)
		return out, nil
	}

	// Update the container
	if err := s.updateContainer(ctx, *targetContainer, inspect, normalizedRef); err != nil {
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "failed",
			Error:        err.Error(),
		})
		out.Failed++
	} else {
		out.Items = append(out.Items, updater.ResourceResult{
			ResourceID:   targetContainer.ID,
			ResourceType: "container",
			ResourceName: containerName,
			Status:       "updated",
		})
		out.Updated++

		// Clear the update record for this image
		if err := s.clearImageUpdateRecord(ctx, repo, tag); err != nil {
			slog.WarnContext(ctx, "failed to clear update record", "repo", repo, "tag", tag, "err", err)
		}
	}

	out.Checked = 1
	out.Duration = time.Since(start).String()

	slog.InfoContext(ctx, "UpdateSingleContainer: complete", "containerID", containerID, "updated", out.Updated, "failed", out.Failed)

	return out, nil
}

func (s *UpdaterService) pruneImageIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	slog.DebugContext(ctx, "pruneImageIDs: attempting to prune image ids", "count", len(ids))

	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("docker connect: %w", err)
	}

	for _, id := range ids {
		if id == "" {
			continue
		}

		slog.DebugContext(ctx, "pruneImageIDs: checking image id", "imageId", id)

		inUse, err := s.anyImageIDsStillInUse(ctx, []string{id})
		if err != nil {
			slog.Warn("check image usage failed", "imageId", id, "err", err)
			continue
		}
		if inUse {
			slog.DebugContext(ctx, "pruneImageIDs: image still in use, skipping", "imageId", id)
			// still referenced by a container; skip
			continue
		}

		if _, err := dcli.ImageRemove(ctx, id, image.RemoveOptions{PruneChildren: true}); err != nil {
			slog.Warn("image remove failed", "imageId", id, "err", err)
			continue
		}

		s.logAutoUpdate(ctx, models.EventSeverityInfo, models.JSON{
			"phase":   "image_prune",
			"imageId": id,
			"status":  "removed",
		})
		slog.DebugContext(ctx, "pruneImageIDs: image removed", "imageId", id)
	}

	return nil
}

func (s *UpdaterService) GetStatus() updater.Status {
	containerIDs := make([]string, 0, len(s.updatingContainers))
	for id := range s.updatingContainers {
		containerIDs = append(containerIDs, id)
	}
	projectIDs := make([]string, 0, len(s.updatingProjects))
	for id := range s.updatingProjects {
		projectIDs = append(projectIDs, id)
	}

	return updater.Status{
		UpdatingContainers: len(s.updatingContainers),
		UpdatingProjects:   len(s.updatingProjects),
		ContainerIds:       containerIDs,
		ProjectIds:         projectIDs,
	}
}

func (s *UpdaterService) GetHistory(ctx context.Context, limit int) ([]models.AutoUpdateRecord, error) {
	var rec []models.AutoUpdateRecord
	q := s.db.WithContext(ctx).Order("start_time DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rec).Error; err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	return rec, nil
}

// --- internals ---

//nolint:gocognit
func (s *UpdaterService) updateContainer(ctx context.Context, cnt container.Summary, inspect container.InspectResponse, newRef string) error {
	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("docker connect: %w", err)
	}

	name := s.getContainerName(cnt)
	labels := inspect.Config.Labels
	isArcane := arcaneupdater.IsArcaneContainer(labels)

	// Arcane containers should always use CLI upgrade, not inline update
	// This method should not be called for Arcane containers
	if isArcane {
		slog.ErrorContext(ctx, "updateContainer: called for Arcane container - should use CLI upgrade instead", "containerId", cnt.ID, "containerName", name)
		return fmt.Errorf("arcane containers must use CLI upgrade method (TriggerUpgradeViaCLI), not inline update")
	}

	slog.DebugContext(ctx, "updateContainer: starting update", "containerId", cnt.ID, "containerName", name, "newRef", newRef, "isArcane", isArcane)

	// Execute pre-update lifecycle hook
	hookResult := arcaneupdater.ExecutePreUpdateCommand(ctx, dcli, cnt.ID, labels)
	if hookResult.Executed {
		if hookResult.SkipUpdate {
			slog.InfoContext(ctx, "updateContainer: container requested skip via pre-update hook", "containerId", cnt.ID, "containerName", name)
			return fmt.Errorf("container requested skip update via exit code %d", arcaneupdater.ExitCodeSkipUpdate)
		}
		if hookResult.Error != nil {
			slog.WarnContext(ctx, "updateContainer: pre-update hook failed", "containerId", cnt.ID, "err", hookResult.Error)
			// Continue with update despite hook failure (configurable in future)
		}
	}

	originalName := inspect.Name

	// Get custom stop signal if configured
	stopSignal := arcaneupdater.GetStopSignal(labels)
	stopOpts := container.StopOptions{}
	if stopSignal != "" {
		stopOpts.Signal = stopSignal
		slog.DebugContext(ctx, "updateContainer: using custom stop signal", "signal", stopSignal)
	}

	// Stop the container
	if err := dcli.ContainerStop(ctx, cnt.ID, stopOpts); err != nil {
		slog.DebugContext(ctx, "updateContainer: stop failed", "containerId", cnt.ID, "err", err)
		return fmt.Errorf("stop: %w", err)
	}
	_ = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStop, cnt.ID, name, systemUser.ID, systemUser.Username, "0", models.JSON{"action": "updater_stop"})

	// Remove the container
	if err := dcli.ContainerRemove(ctx, cnt.ID, container.RemoveOptions{}); err != nil {
		slog.DebugContext(ctx, "updateContainer: remove failed", "containerId", cnt.ID, "err", err)
		return fmt.Errorf("remove: %w", err)
	}
	_ = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDelete, cnt.ID, name, systemUser.ID, systemUser.Username, "0", models.JSON{"action": "updater_delete"})

	// recreate with new image ref
	cfg := inspect.Config
	cfg.Image = newRef

	// Fix for "conflicting options: hostname and the network mode"
	// When network mode is "host" or "container:...", Hostname must be empty
	nm := inspect.HostConfig.NetworkMode
	if nm.IsHost() || nm.IsContainer() {
		cfg.Hostname = ""
		cfg.Domainname = ""
	}

	// Fix for "conflicting options: port exposing and the container type network mode"
	// When network mode is "container:...", port mappings are not allowed
	if nm.IsContainer() {
		cfg.ExposedPorts = nil
		inspect.HostConfig.PortBindings = nil
		inspect.HostConfig.PublishAllPorts = false
	}

	var networkingConfig *network.NetworkingConfig
	if !nm.IsContainer() {
		networkingConfig = &network.NetworkingConfig{EndpointsConfig: inspect.NetworkSettings.Networks}
	}

	// Use original name for new container
	containerName := strings.TrimPrefix(originalName, "/")

	resp, err := dcli.ContainerCreate(ctx, cfg, inspect.HostConfig, networkingConfig, nil, containerName)
	if err != nil {
		slog.DebugContext(ctx, "updateContainer: create failed", "containerName", containerName, "err", err)
		return fmt.Errorf("create: %w", err)
	}
	_ = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerCreate, resp.ID, name, systemUser.ID, systemUser.Username, "0", models.JSON{"action": "updater_create", "newImageId": resp.ID})

	if err := dcli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		slog.DebugContext(ctx, "updateContainer: start failed", "newContainerId", resp.ID, "err", err)
		return fmt.Errorf("start: %w", err)
	}
	_ = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStart, resp.ID, name, systemUser.ID, systemUser.Username, "0", models.JSON{"action": "updater_start"})

	// Execute post-update lifecycle hook on the new container
	hookResult = arcaneupdater.ExecutePostUpdateCommand(ctx, dcli, resp.ID, labels)
	if hookResult.Executed && hookResult.Error != nil {
		slog.WarnContext(ctx, "updateContainer: post-update hook failed", "newContainerId", resp.ID, "err", hookResult.Error)
		// Log but don't fail the update
	}

	_ = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerUpdate, resp.ID, name, systemUser.ID, systemUser.Username, "0", models.JSON{
		"oldContainerId": cnt.ID,
		"newContainerId": resp.ID,
		"newImage":       newRef,
	})

	slog.DebugContext(ctx, "updateContainer: update complete", "oldContainerId", cnt.ID, "newContainerId", resp.ID)
	return nil
}

// normalizeRef returns a canonical "registry/repository:tag" without digest.
// Examples:
// - "redis:latest" -> "docker.io/library/redis:latest"
// - "nginx@sha256:..." -> "docker.io/library/nginx:latest" (if no tag was present, defaults to latest)
func (s *UpdaterService) normalizeRef(ref string) string {
	ref = s.stripDigest(ref)

	// Split tag
	tag := "latest"
	if i := strings.LastIndex(ref, ":"); i != -1 && strings.LastIndex(ref, "/") < i {
		tag = ref[i+1:]
		ref = ref[:i]
	}

	parts := strings.Split(ref, "/")
	domain := ""
	switch {
	case len(parts) > 0 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost"):
		domain = strings.ToLower(parts[0])
		parts = parts[1:]
	default:
		domain = "docker.io"
	}
	repo := strings.Join(parts, "/")
	if domain == "docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}

	// Canonical docker.io domain
	switch domain {
	case "index.docker.io", "registry-1.docker.io":
		domain = "docker.io"
	}
	return strings.ToLower(domain + "/" + repo + ":" + tag)
}

func (s *UpdaterService) stripDigest(ref string) string {
	if i := strings.Index(ref, "@"); i != -1 {
		return ref[:i]
	}
	return ref
}

// collectUsedImagesFromContainers adds normalized image tags from non-opted-out running containers.
func (s *UpdaterService) collectUsedImagesFromContainers(ctx context.Context, dcli *client.Client, out map[string]struct{}) error {
	if dcli == nil {
		return nil
	}
	list, err := dcli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "collectUsedImagesFromContainers: container list fetched", "count", len(list))
	for _, c := range list {
		if arcaneupdater.IsUpdateDisabled(c.Labels) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container opted out by labels", "containerId", c.ID)
			continue
		}
		inspect, err := dcli.ContainerInspect(ctx, c.ID)
		if err != nil {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect failed", "containerId", c.ID, "err", err)
			continue
		}
		if inspect.Config != nil && arcaneupdater.IsUpdateDisabled(inspect.Config.Labels) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect labels opted out", "containerId", c.ID)
			continue
		}
		for _, t := range s.getNormalizedTagsForContainer(ctx, dcli, inspect) {
			out[t] = struct{}{}
		}
	}
	return nil
}

// Aggregate images in use across containers and compose projects
func (s *UpdaterService) collectUsedImages(ctx context.Context) (map[string]struct{}, error) {
	out := map[string]struct{}{}

	dcli, err := s.dockerService.GetClient()
	if err == nil && dcli != nil {

		slog.DebugContext(ctx, "collectUsedImages: docker connection created")
	} else {
		slog.DebugContext(ctx, "collectUsedImages: docker connection not available, continuing without container list", "err", err)
	}

	_ = s.collectUsedImagesFromContainers(ctx, dcli, out)
	_ = s.collectUsedImagesFromProjects(ctx, out)

	slog.DebugContext(ctx, "collectUsedImages: collected used images", "count", len(out))
	return out, nil
}

func (s *UpdaterService) collectUsedImagesFromProjects(ctx context.Context, out map[string]struct{}) error {
	if s.projectService == nil {
		return nil
	}

	projs, err := s.projectService.ListAllProjects(ctx)
	if err != nil {
		return err
	}

	for _, p := range projs {
		// consider running and partially running projects
		if p.Status != models.ProjectStatusRunning && p.Status != models.ProjectStatusPartiallyRunning {
			continue
		}

		services, serr := s.projectService.GetProjectServices(ctx, p.ID)
		if serr != nil {
			continue
		}
		for _, svc := range services {
			if svc.ServiceConfig != nil && arcaneupdater.IsUpdateDisabled(svc.ServiceConfig.Labels) {
				continue
			}
			img := strings.TrimSpace(svc.Image)
			if img == "" {
				continue
			}
			out[s.normalizeRef(img)] = struct{}{}
		}
	}
	return nil
}

func (s *UpdaterService) getNormalizedTagsForContainer(ctx context.Context, dcli *client.Client, inspect container.InspectResponse) []string {
	seen := map[string]struct{}{}

	// Prefer tags from the image object (handles sha256 IDs)
	if dcli != nil {
		if ii, err := dcli.ImageInspect(ctx, inspect.Image); err == nil {
			slog.DebugContext(ctx, "getNormalizedTagsForContainer: image inspect success", "imageId", inspect.Image, "repoTags", len(ii.RepoTags))
			for _, tag := range ii.RepoTags {
				if tag == "<none>:<none>" || tag == "" {
					continue
				}
				seen[s.normalizeRef(tag)] = struct{}{}
			}
		} else {
			slog.DebugContext(ctx, "getNormalizedTagsForContainer: image inspect failed", "imageId", inspect.Image, "err", err)
		}
	}

	if inspect.Config != nil && inspect.Config.Image != "" {
		seen[s.normalizeRef(inspect.Config.Image)] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slog.DebugContext(ctx, "getNormalizedTagsForContainer: normalized tags", "count", len(out))
	return out
}

func (s *UpdaterService) getContainerName(cnt container.Summary) string {
	if len(cnt.Names) > 0 {
		n := cnt.Names[0]
		if strings.HasPrefix(n, "/") {
			return n[1:]
		}
		return n
	}
	return cnt.ID[:12]
}

func (s *UpdaterService) recordRun(ctx context.Context, item updater.ResourceResult) error {
	rec := &models.AutoUpdateRecord{
		ResourceID:      item.ResourceID,
		ResourceType:    item.ResourceType,
		ResourceName:    item.ResourceName,
		Status:          models.AutoUpdateStatus(item.Status),
		StartTime:       time.Now(),
		UpdateAvailable: item.Status == "updated" || item.Status == "update_available",
		UpdateApplied:   item.UpdateApplied,
	}

	if item.Error != "" {
		rec.Error = &item.Error
	}

	if len(item.OldImages) > 0 {
		old := make(models.JSON)
		for k, v := range item.OldImages {
			old[k] = v
		}
		rec.OldImageVersions = old
	}
	if len(item.NewImages) > 0 {
		newv := make(models.JSON)
		for k, v := range item.NewImages {
			newv[k] = v
		}
		rec.NewImageVersions = newv
	}

	end := time.Now()
	rec.EndTime = &end

	return s.db.WithContext(ctx).Create(rec).Error
}

// Resolve the local image ID(s) currently referenced by ref (repo:tag) before we pull.
// Returns IDs like "sha256:...".
func (s *UpdaterService) resolveLocalImageIDsForRef(ctx context.Context, ref string) ([]string, error) {
	slog.DebugContext(ctx, "resolveLocalImageIDsForRef: resolving local image ids for ref", "ref", ref)

	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return nil, err
	}

	checker := arcaneupdater.NewDigestChecker(dcli, arcRegistry.NewClient())
	ids, err := checker.GetImageIDsForRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "resolveLocalImageIDsForRef: resolved ids", "ref", ref, "ids", ids)
	return ids, nil
}

//nolint:gocognit
func (s *UpdaterService) restartContainersUsingOldIDs(ctx context.Context, oldIDToNewRef map[string]string, oldRefToNewRef map[string]string) ([]updater.ResourceResult, error) {
	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("docker connect: %w", err)
	}

	list, err := dcli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	slog.DebugContext(ctx, "restartContainersUsingOldIDs: scanning containers for matching images", "containers", len(list), "oldIDMatches", len(oldIDToNewRef), "oldRefMatches", len(oldRefToNewRef))

	updatedNorm := map[string]string{}
	for oldRef, nr := range oldRefToNewRef {
		updatedNorm[s.normalizeRef(oldRef)] = nr
	}

	type restartPlan struct {
		cnt      container.Summary
		inspect  container.InspectResponse
		newRef   string
		match    string
		explicit bool
		implicit bool
	}

	plansByName := map[string]*restartPlan{}
	markedForRestart := map[string]bool{}
	containersWithDeps := make([]arcaneupdater.ContainerWithDeps, 0, len(list))

	// Cache resolved IDs for newRefs to avoid repeated API calls
	targetImageIDs := map[string][]string{}

	for _, c := range list {
		inspect, err := dcli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		labels := c.Labels
		if inspect.Config != nil && inspect.Config.Labels != nil {
			labels = inspect.Config.Labels
		}

		// Skip containers with opt-out label
		if arcaneupdater.IsUpdateDisabled(labels) {
			continue
		}

		// Ensure labels map exists to avoid nil panics in implicit restart marking
		if c.Labels == nil {
			c.Labels = map[string]string{}
		}

		dep := arcaneupdater.ExtractContainerDeps(ctx, dcli, c, inspect)
		containersWithDeps = append(containersWithDeps, dep)

		var (
			newRef string
			match  string
		)

		// Primary: match by digest (image ID)
		if nr, ok := oldIDToNewRef[inspect.Image]; ok {
			newRef = nr
			match = inspect.Image
		} else {
			// Fallback: resolve tags and match by tag
			for _, t := range s.getNormalizedTagsForContainer(ctx, dcli, inspect) {
				if nr, ok := updatedNorm[t]; ok {
					newRef = nr
					match = t
					break
				}
			}
		}

		if newRef != "" {
			// Check if container is already on the target image
			tids, cached := targetImageIDs[newRef]
			if !cached {
				tids, _ = s.resolveLocalImageIDsForRef(ctx, newRef)
				targetImageIDs[newRef] = tids
			}

			for _, tid := range tids {
				if tid == inspect.Image {
					// Already on target image
					slog.InfoContext(ctx, "restartContainersUsingOldIDs: container already on target image; skipping restart",
						"containerId", c.ID, "containerName", dep.Name, "imageID", inspect.Image, "newRef", newRef)
					newRef = ""
					break
				}
			}
		}

		p := &restartPlan{cnt: c, inspect: inspect, newRef: newRef, match: match, explicit: newRef != ""}
		plansByName[dep.Name] = p
		if p.explicit {
			markedForRestart[dep.Name] = true
		}
	}

	// Propagate implicit restarts: if a dependency is restarting, restart dependents too.
	for {
		added := arcaneupdater.UpdateImplicitRestart(containersWithDeps, markedForRestart)
		if len(added) == 0 {
			break
		}
		for _, name := range added {
			if p, ok := plansByName[name]; ok {
				if p.newRef == "" {
					if p.inspect.Config != nil {
						p.newRef = strings.TrimSpace(p.inspect.Config.Image)
					}
					if p.newRef == "" {
						p.newRef = p.cnt.Image
					}
					p.match = "dependency_restart"
					p.implicit = true
				}
			}
		}
	}

	// Build the set of containers that will be restarted and sort them by dependency order.
	candidates := make([]arcaneupdater.ContainerWithDeps, 0, len(containersWithDeps))
	for _, cd := range containersWithDeps {
		if markedForRestart[cd.Name] {
			candidates = append(candidates, cd)
		}
	}

	sorter := arcaneupdater.NewContainerSorter(candidates)
	sorted, sortErr := sorter.Sort()
	_, _ = sorter.SortReverse() // keep method used; reverse order may be useful for future stop-first flows
	if sortErr != nil {
		slog.WarnContext(ctx, "restartContainersUsingOldIDs: dependency sort failed, falling back to unsorted order", "error", sortErr.Error())
		sorted = candidates
	}

	var results []updater.ResourceResult
	for _, cd := range sorted {
		p := plansByName[cd.Name]
		if p == nil {
			continue
		}

		name := cd.Name
		labels := map[string]string{}
		if p.inspect.Config != nil && p.inspect.Config.Labels != nil {
			labels = p.inspect.Config.Labels
		}

		res := updater.ResourceResult{
			ResourceID:   p.cnt.ID,
			ResourceName: name,
			ResourceType: "container",
			Status:       "checked",
			OldImages:    map[string]string{"main": p.match},
			NewImages:    map[string]string{"main": s.normalizeRef(p.newRef)},
		}

		// Lifecycle hooks for "check" phase (best-effort)
		if !arcaneupdater.IsArcaneContainer(labels) {
			pre := arcaneupdater.ExecutePreCheckCommand(ctx, dcli, p.cnt.ID, labels)
			if pre.Executed {
				if pre.SkipUpdate {
					res.Status = "skipped"
					res.Error = fmt.Sprintf("container requested skip via pre-check hook (exit %d)", arcaneupdater.ExitCodeSkipUpdate)
					results = append(results, res)
					continue
				}
				if pre.Error != nil {
					slog.WarnContext(ctx, "restartContainersUsingOldIDs: pre-check hook failed", "container", name, "error", pre.Error.Error())
				}
			}
			post := arcaneupdater.ExecutePostCheckCommand(ctx, dcli, p.cnt.ID, labels)
			if post.Executed && post.Error != nil {
				slog.WarnContext(ctx, "restartContainersUsingOldIDs: post-check hook failed", "container", name, "error", post.Error.Error())
			}
		}

		if p.newRef == "" {
			res.Status = "skipped"
			res.Error = "no matching updated image"
			results = append(results, res)
			continue
		}

		slog.DebugContext(ctx, "restartContainersUsingOldIDs: restarting container", "containerId", p.cnt.ID, "container", name, "match", p.match, "newRef", p.newRef, "implicit", p.implicit)

		// Check if this is Arcane self-update - use CLI upgrade instead
		if arcaneupdater.IsArcaneContainer(labels) && s.upgradeService != nil {
			slog.InfoContext(ctx, "restartContainersUsingOldIDs: detected Arcane self-update, using CLI upgrade method", "containerId", p.cnt.ID, "container", name)

			if err := s.upgradeService.TriggerUpgradeViaCLI(ctx, systemUser); err != nil {
				res.Status = "failed"
				res.Error = fmt.Sprintf("CLI upgrade failed: %v", err)
				slog.WarnContext(ctx, "restartContainersUsingOldIDs: CLI upgrade failed", "containerId", p.cnt.ID, "err", err)
			} else {
				res.Status = "updated"
				res.UpdateAvailable = true
				res.UpdateApplied = true
				slog.InfoContext(ctx, "restartContainersUsingOldIDs: CLI upgrade triggered successfully", "containerId", p.cnt.ID)
			}
		} else if err := s.updateContainer(ctx, p.cnt, p.inspect, p.newRef); err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			slog.DebugContext(ctx, "restartContainersUsingOldIDs: update failed", "containerId", p.cnt.ID, "err", err)
		} else {
			res.Status = "updated"
			res.UpdateAvailable = true
			res.UpdateApplied = true
			slog.DebugContext(ctx, "restartContainersUsingOldIDs: update succeeded", "containerId", p.cnt.ID)

			// Send notification after successful container update
			if s.notificationService != nil {
				if notifErr := s.notificationService.SendContainerUpdateNotification(ctx, name, p.newRef, p.match, s.normalizeRef(p.newRef)); notifErr != nil {
					slog.WarnContext(ctx, "Failed to send container update notification", "containerId", p.cnt.ID, "containerName", name, "imageRef", p.newRef, "error", notifErr.Error())
				}
			}
		}
		results = append(results, res)
	}
	slog.DebugContext(ctx, "restartContainersUsingOldIDs: completed scanning", "results", len(results))
	return results, nil
}

// parseNormalizedRef expects a normalized ref in the form "host/repository:tag".
func (s *UpdaterService) parseNormalizedRef(ref string) (host, repository, tag string) {
	// host/repo:tag
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", "", ""
	}
	host = parts[0]
	rest := parts[1]
	tag = "latest"
	if i := strings.LastIndex(rest, ":"); i != -1 && strings.LastIndex(rest, "/") < i {
		tag = rest[i+1:]
		repository = rest[:i]
	} else {
		repository = rest
	}
	return host, repository, tag
}

func (s *UpdaterService) logAutoUpdate(ctx context.Context, sev models.EventSeverity, metadata models.JSON) {
	phase, _ := metadata["phase"].(string)

	title := "Auto-update"
	switch phase {
	case "start":
		title = "Auto-update run started"
	case "image_pull":
		img := fmt.Sprint(metadata["imageNew"])
		if img == "" {
			img = fmt.Sprint(metadata["imageOld"])
		}
		if img != "" {
			title = fmt.Sprintf("Auto-update: image pull %s", img)
		} else {
			title = "Auto-update: image pull"
		}
	case "image_prune":
		imageID := fmt.Sprint(metadata["imageId"])
		if imageID != "" {
			title = fmt.Sprintf("Auto-update: image prune %s", imageID)
		} else {
			title = "Auto-update: image prune"
		}
	case "container":
		name := fmt.Sprint(metadata["container"])
		if name == "" {
			name = fmt.Sprint(metadata["containerId"])
		}
		if name != "" {
			title = fmt.Sprintf("Auto-update: container %s", name)
		} else {
			title = "Auto-update: container"
		}
	case "project":
		name := fmt.Sprint(metadata["projectName"])
		if name == "" {
			name = fmt.Sprint(metadata["projectId"])
		}
		if name != "" {
			title = fmt.Sprintf("Auto-update: project %s", name)
		} else {
			title = "Auto-update: project"
		}
	case "complete":
		title = "Auto-update run completed"
	}

	resourceType := "system"
	resourceName := "auto_updater"
	environmentID := "0"

	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeSystemAutoUpdate,
		Severity:      sev,
		Title:         title,
		ResourceType:  &resourceType,
		ResourceName:  &resourceName,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
}

func (s *UpdaterService) severityFromStatus(status string) models.EventSeverity {
	switch status {
	case "failed":
		return models.EventSeverityError
	case "updated":
		return models.EventSeveritySuccess
	default:
		return models.EventSeverityInfo
	}
}

func (s *UpdaterService) anyImageIDsStillInUse(ctx context.Context, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = struct{}{}
		}
	}

	slog.DebugContext(ctx, "anyImageIDsStillInUse: checking ids", "ids", ids)

	dcli, err := s.dockerService.GetClient()
	if err != nil {
		return false, fmt.Errorf("docker connect: %w", err)
	}

	list, err := dcli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return false, fmt.Errorf("list containers: %w", err)
	}
	for _, c := range list {
		inspect, ierr := dcli.ContainerInspect(ctx, c.ID)
		if ierr != nil {
			continue
		}
		if _, ok := set[inspect.Image]; ok {
			slog.DebugContext(ctx, "anyImageIDsStillInUse: image still used by container", "imageId", inspect.Image, "containerId", c.ID)
			return true, nil
		}
	}
	slog.DebugContext(ctx, "anyImageIDsStillInUse: no matching usage found")
	return false, nil
}

func (s *UpdaterService) clearImageUpdateRecord(ctx context.Context, repository, tag string) error {
	return s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Where("repository = ? AND tag = ?", repository, tag).
		Update("has_update", false).Error
}

// parseRepoAndTag extracts repository and tag from a reference like "registry/repo:tag".
// Uses the last ":" occurring after the last "/" as the tag separator. Defaults tag to "latest".
func (s *UpdaterService) parseRepoAndTag(ref string) (string, string) {
	// strip digest if present
	ref = s.stripDigest(ref)

	tag := "latest"
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash && colon != -1 {
		tag = ref[colon+1:]
		ref = ref[:colon]
	}
	// Keep registry in repository as stored in records (they store Repository without tag)
	return ref, tag
}
