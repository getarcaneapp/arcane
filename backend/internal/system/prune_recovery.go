package system

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
)

type pruneResourceInternal struct {
	Kind  string   `json:"kind"`
	ID    string   `json:"id"`
	Size  int64    `json:"size"`
	Names []string `json:"names,omitempty"`
}
type prunePlanInternal struct {
	Request   systemtypes.PruneAllRequest `json:"request"`
	Resources []pruneResourceInternal     `json:"resources"`
}

// PruneScheduled freezes exact targets before applying destructive work.
func (s *SystemService) PruneScheduled(ctx context.Context, environmentID string, req systemtypes.PruneAllRequest) (*systemtypes.PruneAllResult, bool, error) {
	prune := s.beginSystemPruneInternal(ctx, environmentID, req)
	result := &systemtypes.PruneAllResult{Success: true}
	if prune.activityID != "" {
		result.ActivityID = &prune.activityID
	}
	if prune.started.IsAbsent() {
		return result, false, nil
	}
	defer s.finishSystemPruneInternal(environmentID)
	fail := func(err error) (*systemtypes.PruneAllResult, bool, error) {
		result.Success = false
		result.Errors = append(result.Errors, err.Error())
		s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
		return result, true, err
	}

	plan, err := s.preparePrunePlanInternal(ctx, req)
	if err != nil {
		return fail(err)
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fail(err)
	}
	if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ResourceType: "prune_plan", ID: "prune-plan", Status: schedulertypes.Succeeded, RecoveryData: payload, ActivityID: prune.activityID}); err != nil {
		return fail(err)
	}
	previous, _ := jobcontext.Run(ctx)
	err = s.executePrunePlanInternal(ctx, plan, previous, result, false)
	s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
	if err == nil && result.Success {
		err = jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: "scheduled-prune", Status: schedulertypes.Succeeded, ActivityID: prune.activityID})
	}
	return result, true, err
}

func (s *SystemService) preparePrunePlanInternal(ctx context.Context, req systemtypes.PruneAllRequest) (prunePlanInternal, error) {
	plan := prunePlanInternal{Request: req}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return plan, err
	}
	if req.Volumes != nil {
		s.volumeService.CleanupHelperContainers(ctx)
	}
	usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{Containers: true, Images: true, Volumes: true, BuildCache: req.BuildCache != nil, Verbose: true})
	if err != nil {
		return plan, err
	}
	if err := s.selectPruneContainersInternal(&plan, usage); err != nil {
		return plan, err
	}
	selectedContainers, imageRefs, volumeRefs := pruneSelectedReferencesInternal(plan.Resources, usage)
	if err := s.selectPruneImagesInternal(&plan, usage, imageRefs); err != nil {
		return plan, err
	}
	selectPruneVolumesInternal(&plan, usage, volumeRefs)
	if err := s.selectPruneNetworksInternal(ctx, dockerClient, &plan, selectedContainers); err != nil {
		return plan, err
	}
	if err := s.selectPruneBuildCacheInternal(&plan, usage); err != nil {
		return plan, err
	}
	return plan, nil
}

func (s *SystemService) selectPruneContainersInternal(plan *prunePlanInternal, usage client.DiskUsageResult) error {
	req := plan.Request
	var err error
	if req.Containers == nil || req.Containers.Mode == systemtypes.PruneContainerModeNone {
		return nil
	}
	until := time.Time{}
	if req.Containers.Mode == systemtypes.PruneContainerModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Containers.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.Containers.Items {
		if item.State == "running" || item.State == "paused" || item.State == "restarting" || (!until.IsZero() && !time.Unix(item.Created, 0).Before(until)) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "container", ID: item.ID, Size: item.SizeRw})
	}
	return nil
}

func (s *SystemService) selectPruneImagesInternal(plan *prunePlanInternal, usage client.DiskUsageResult, imageRefs map[string]int) error {
	req := plan.Request
	var err error
	if req.Images == nil || req.Images.Mode == systemtypes.PruneImageModeNone {
		return nil
	}
	until := time.Time{}
	if req.Images.Mode == systemtypes.PruneImageModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Images.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.Images.Items {
		if item.Containers > int64(imageRefs[item.ID]) || (req.Images.Mode == systemtypes.PruneImageModeDangling && len(item.RepoTags) > 0) || (!until.IsZero() && !time.Unix(item.Created, 0).Before(until)) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "image", ID: item.ID, Size: item.Size, Names: slices.Clone(item.RepoTags)})
	}
	return nil
}

func selectPruneVolumesInternal(plan *prunePlanInternal, usage client.DiskUsageResult, volumeRefs map[string]int) {
	req := plan.Request
	if req.Volumes != nil && req.Volumes.Mode != systemtypes.PruneVolumeModeNone {
		for _, item := range usage.Volumes.Items {
			if item.UsageData == nil || item.UsageData.RefCount > int64(volumeRefs[item.Name]) || strings.EqualFold(item.Labels[libarcane.InternalResourceLabel], "true") {
				continue
			}
			if req.Volumes.Mode == systemtypes.PruneVolumeModeAnonymous {
				if _, anonymous := item.Labels["com.docker.volume.anonymous"]; !anonymous {
					continue
				}
			}
			plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "volume", ID: item.Name, Size: item.UsageData.Size})
		}
	}
}

func (s *SystemService) selectPruneNetworksInternal(ctx context.Context, dockerClient *client.Client, plan *prunePlanInternal, selectedContainers map[string]bool) error {
	req := plan.Request
	var err error
	if req.Networks == nil || req.Networks.Mode == systemtypes.PruneNetworkModeNone {
		return nil
	}
	until := time.Time{}
	if req.Networks.Mode == systemtypes.PruneNetworkModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Networks.Until)
		if err != nil {
			return err
		}
	}
	networks, err := dockerClient.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return err
	}
	for _, item := range networks.Items {
		if item.Name == "bridge" || item.Name == "host" || item.Name == "none" || item.Ingress || item.Scope != "local" || (!until.IsZero() && !item.Created.Before(until)) {
			continue
		}
		inspect, err := compat.NetworkInspectWithCompatibility(ctx, dockerClient, item.ID, client.NetworkInspectOptions{})
		if err != nil {
			return err
		}
		eligible := true
		for id := range inspect.Network.Containers {
			if !selectedContainers[id] {
				eligible = false
				break
			}
		}
		if !eligible {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "network", ID: item.ID})
	}
	return nil
}

func (s *SystemService) selectPruneBuildCacheInternal(plan *prunePlanInternal, usage client.DiskUsageResult) error {
	req := plan.Request
	var err error
	if req.BuildCache == nil || req.BuildCache.Mode == systemtypes.PruneBuildCacheModeNone {
		return nil
	}
	until := time.Time{}
	if req.BuildCache.Mode == systemtypes.PruneBuildCacheModeOlderThan {
		until, err = s.pruneCutoffInternal(req.BuildCache.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.BuildCache.Items {
		if item.InUse || (!until.IsZero() && (item.LastUsedAt == nil || !item.LastUsedAt.Before(until))) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "build_cache", ID: item.ID, Size: item.Size})
	}
	return nil
}

func pruneSelectedReferencesInternal(resources []pruneResourceInternal, usage client.DiskUsageResult) (map[string]bool, map[string]int, map[string]int) {
	selectedContainers := make(map[string]bool)
	imageRefs := make(map[string]int)
	volumeRefs := make(map[string]int)
	for _, resource := range resources {
		if resource.Kind == "container" {
			selectedContainers[resource.ID] = true
		}
	}
	for _, item := range usage.Containers.Items {
		if !selectedContainers[item.ID] {
			continue
		}
		imageRefs[item.ImageID]++
		mounted := make(map[string]bool)
		for _, m := range item.Mounts {
			if m.Type == "volume" && !mounted[m.Name] {
				mounted[m.Name] = true
				volumeRefs[m.Name]++
			}
		}
	}
	return selectedContainers, imageRefs, volumeRefs
}

func (s *SystemService) pruneCutoffInternal(raw string) (time.Time, error) {
	now := time.Now()
	location := s.location
	if location == nil {
		location = time.UTC
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration >= 0 {
		return now.Add(-duration), nil
	}
	for _, format := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if value, err := time.ParseInLocation(format, raw, location); err == nil {
			return value, nil
		}
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		return time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9)), nil
	}
	return time.Time{}, fmt.Errorf("unsupported prune cutoff %q", raw)
}

func (s *SystemService) executePrunePlanInternal(ctx context.Context, plan prunePlanInternal, previous schedulertypes.Run, result *systemtypes.PruneAllResult, recovery bool) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return err
	}
	for _, resource := range plan.Resources {
		if resource.Kind == "build_cache" {
			continue
		}
		target := schedulertypes.TargetOutcome{ResourceType: resource.Kind, ID: "prune:" + resource.Kind + ":" + resource.ID, Status: schedulertypes.Running}
		if pruneTargetCompletedInternal(previous, target.ID) {
			continue
		}
		if err := jobcontext.Progress(ctx, target); err != nil {
			return err
		}
		eligible, exists, err := s.pruneResourceEligibleInternal(ctx, dockerClient, plan, resource)
		if err != nil {
			return err
		}
		switch {
		case !exists:
			target.Status = schedulertypes.Succeeded
		case !eligible:
			target.Status = schedulertypes.Skipped
		default:
			err = s.removePruneResourceInternal(ctx, dockerClient, resource)
			if err != nil && !errdefs.IsNotFound(err) {
				target.Status = schedulertypes.Failed
				target.Message = err.Error()
				result.Success = false
				result.Errors = append(result.Errors, err.Error())
			} else {
				target.Status = schedulertypes.Succeeded
				recordPrunedResourceInternal(resource, result)
			}
		}
		if err := jobcontext.Progress(ctx, target); err != nil {
			return err
		}
	}
	if len(result.ImagesDeleted) > 0 {
		if err := s.imageUpdateService.DeleteRecordsForImages(ctx, result.ImagesDeleted); err != nil {
			return err
		}
	}
	return s.executePruneCacheInternal(ctx, dockerClient, plan, result, recovery)
}

func pruneTargetCompletedInternal(previous schedulertypes.Run, targetID string) bool {
	for _, target := range previous.Outcome.Targets {
		if target.ID == targetID && (target.Status == schedulertypes.Succeeded || target.Status == schedulertypes.Skipped) {
			return true
		}
	}
	return false
}

func (s *SystemService) removePruneResourceInternal(ctx context.Context, dockerClient *client.Client, resource pruneResourceInternal) error {
	var err error
	switch resource.Kind {
	case "container":
		_, err = dockerClient.ContainerRemove(ctx, resource.ID, client.ContainerRemoveOptions{})
	case "image":
		err = removePruneImageInternal(ctx, dockerClient, resource)
	case "volume":
		err = s.volumeService.DeleteVolume(ctx, resource.ID, false, common.SystemUser)
	case "network":
		err = s.networkService.RemoveNetwork(ctx, resource.ID, common.SystemUser)
	default:
		return fmt.Errorf("unknown prune target %q", resource.Kind)
	}
	return err
}

func removePruneImageInternal(ctx context.Context, dockerClient *client.Client, resource pruneResourceInternal) error {
	var err error
	for _, name := range resource.Names {
		inspected, inspectErr := dockerClient.ImageInspect(ctx, name)
		if errdefs.IsNotFound(inspectErr) {
			continue
		}
		if inspectErr != nil {
			return inspectErr
		}
		if inspected.ID != resource.ID {
			continue
		}
		_, err = dockerClient.ImageRemove(ctx, name, client.ImageRemoveOptions{PruneChildren: false})
		if err != nil {
			return err
		}
	}
	_, err = dockerClient.ImageRemove(ctx, resource.ID, client.ImageRemoveOptions{PruneChildren: false})
	return err
}

func recordPrunedResourceInternal(resource pruneResourceInternal, result *systemtypes.PruneAllResult) {
	switch resource.Kind {
	case "container":
		result.ContainersPruned = append(result.ContainersPruned, resource.ID)
	case "image":
		result.ImagesDeleted = append(result.ImagesDeleted, resource.ID)
		// ImageRemove does not report reclaimed bytes; Size includes shared layers.
		return
	case "volume":
		result.VolumesDeleted = append(result.VolumesDeleted, resource.ID)
	case "network":
		result.NetworksDeleted = append(result.NetworksDeleted, resource.ID)
	}
	if resource.Size <= 0 {
		return
	}
	result.SpaceReclaimed += uint64(resource.Size)
}

func (s *SystemService) executePruneCacheInternal(ctx context.Context, dockerClient *client.Client, plan prunePlanInternal, result *systemtypes.PruneAllResult, recovery bool) error {
	request := plan.Request.BuildCache
	if request == nil || request.Mode == systemtypes.PruneBuildCacheModeNone {
		return nil
	}
	if recovery {
		usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
		if err != nil {
			return err
		}
		selected := make(map[string]bool)
		for _, resource := range plan.Resources {
			if resource.Kind == "build_cache" {
				selected[resource.ID] = true
			}
		}
		for _, current := range usage.BuildCache.Items {
			if selected[current.ID] {
				return errors.New("build cache prune outcome is unconfirmed")
			}
		}
		return nil
	}
	for _, resource := range plan.Resources {
		if resource.Kind != "build_cache" {
			continue
		}
		// Docker interprets the ID filter as a regular expression.
		filters := make(client.Filters).Add("id", "^"+regexp.QuoteMeta(resource.ID)+"$")
		if request.Mode == systemtypes.PruneBuildCacheModeOlderThan {
			filters = filters.Add("until", request.Until)
		}
		report, err := dockerClient.BuildCachePrune(ctx, client.BuildCachePruneOptions{
			All:     request.Mode == systemtypes.PruneBuildCacheModeAll,
			Filters: filters,
		})
		if err != nil {
			return err
		}
		result.SpaceReclaimed += report.Report.SpaceReclaimed
		result.BuildCacheSpaceReclaimed += report.Report.SpaceReclaimed
	}
	return nil
}

func (s *SystemService) pruneResourceEligibleInternal(ctx context.Context, dockerClient *client.Client, plan prunePlanInternal, resource pruneResourceInternal) (eligible, exists bool, err error) {
	if resource.Kind == "network" {
		inspected, err := compat.NetworkInspectWithCompatibility(ctx, dockerClient, resource.ID, client.NetworkInspectOptions{})
		if errdefs.IsNotFound(err) {
			return false, false, nil
		}
		if err != nil {
			return false, false, err
		}
		return len(inspected.Network.Containers) == 0 && !inspected.Network.Ingress, true, nil
	}
	usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{Containers: resource.Kind == "container", Images: resource.Kind == "image", Volumes: resource.Kind == "volume", Verbose: true})
	if err != nil {
		return false, false, err
	}
	switch resource.Kind {
	case "container":
		for _, item := range usage.Containers.Items {
			if item.ID == resource.ID {
				return item.State != "running" && item.State != "paused" && item.State != "restarting", true, nil
			}
		}
	case "image":
		return pruneImageEligibleInternal(usage, plan.Request.Images, resource)
	case "volume":
		for _, item := range usage.Volumes.Items {
			if item.Name == resource.ID {
				return item.UsageData != nil && item.UsageData.RefCount == 0 && !strings.EqualFold(item.Labels[libarcane.InternalResourceLabel], "true"), true, nil
			}
		}
	}
	return false, false, nil
}

func pruneImageEligibleInternal(usage client.DiskUsageResult, request *systemtypes.PruneImagesOptions, resource pruneResourceInternal) (eligible, exists bool, err error) {
	for _, item := range usage.Images.Items {
		if item.ID == resource.ID {
			for _, tag := range item.RepoTags {
				if !slices.Contains(resource.Names, tag) {
					return false, true, nil
				}
			}
			return item.Containers == 0 && (request.Mode != systemtypes.PruneImageModeDangling || len(item.RepoTags) == 0), true, nil
		}
	}
	return false, false, nil
}

func (s *SystemService) ReconcileScheduledPrune(ctx context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, "scheduled-prune")
	if outcome.Status == schedulertypes.Succeeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != "prune-plan" || len(target.RecoveryData) == 0 {
			continue
		}
		var plan prunePlanInternal
		if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
			return outcome, err
		}
		prune := s.beginSystemPruneInternal(ctx, previous.EnvironmentID, plan.Request)
		if prune.started.IsAbsent() {
			return outcome, nil
		}
		defer s.finishSystemPruneInternal(previous.EnvironmentID)
		result := &systemtypes.PruneAllResult{Success: true}
		err := s.executePrunePlanInternal(ctx, plan, previous, result, true)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, err.Error())
		}
		s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
		if err != nil {
			outcome.Message = err.Error()
			return outcome, nil
		}
		if !result.Success {
			return schedulertypes.Outcome{Status: schedulertypes.Partial, Targets: previous.Outcome.Targets}, nil
		}
		if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: "scheduled-prune", Status: schedulertypes.Succeeded}); err != nil {
			return outcome, err
		}
		return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
	}
	return outcome, nil
}
