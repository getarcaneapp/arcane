package services

import (
	"context"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	"golang.org/x/sync/errgroup"
)

const defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour

type DashboardService struct {
	db                   *database.DB
	dockerService        *DockerClientService
	vulnerabilityService *VulnerabilityService
}

type DashboardActionItemsOptions struct {
	DebugAllGood bool
}

func NewDashboardService(
	db *database.DB,
	dockerService *DockerClientService,
	vulnerabilityService *VulnerabilityService,
) *DashboardService {
	return &DashboardService{
		db:                   db,
		dockerService:        dockerService,
		vulnerabilityService: vulnerabilityService,
	}
}

func (s *DashboardService) GetActionItems(ctx context.Context, options DashboardActionItemsOptions) (*dashboardtypes.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}, nil
	}

	var (
		stoppedContainers         int
		pendingImageUpdates       int
		actionableVulnerabilities int
		expiringAPIKeys           int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		count, err := s.getStoppedContainersCountInternal(groupCtx)
		if err != nil {
			return err
		}
		stoppedContainers = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getPendingImageUpdatesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		pendingImageUpdates = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getActionableVulnerabilitiesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		actionableVulnerabilities = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getExpiringAPIKeysCountInternal(groupCtx)
		if err != nil {
			return err
		}
		expiringAPIKeys = count
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	actionItems := make([]dashboardtypes.ActionItem, 0, 4)

	if stoppedContainers > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindStoppedContainers,
			Count:    stoppedContainers,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if pendingImageUpdates > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindImageUpdates,
			Count:    pendingImageUpdates,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if actionableVulnerabilities > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindActionableVulnerabilities,
			Count:    actionableVulnerabilities,
			Severity: dashboardtypes.ActionItemSeverityCritical,
		})
	}

	if expiringAPIKeys > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindExpiringKeys,
			Count:    expiringAPIKeys,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	return &dashboardtypes.ActionItems{Items: actionItems}, nil
}

func (s *DashboardService) getStoppedContainersCountInternal(ctx context.Context) (int, error) {
	if s.dockerService == nil {
		return 0, nil
	}

	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load container counts: %w", err)
	}

	stoppedCount := 0
	for _, container := range containers {
		if libarcane.IsInternalContainer(container.Labels) {
			continue
		}

		if container.State != "running" {
			stoppedCount++
		}
	}

	return stoppedCount, nil
}

func (s *DashboardService) getPendingImageUpdatesCountInternal(ctx context.Context) (int, error) {
	if s.db == nil || s.dockerService == nil {
		return 0, nil
	}

	images, _, _, _, err := s.dockerService.GetAllImages(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load images for update counts: %w", err)
	}

	if len(images) == 0 {
		return 0, nil
	}

	imageIDs := make([]string, 0, len(images))
	for _, img := range images {
		imageIDs = append(imageIDs, img.ID)
	}

	var count int64
	err = s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Where("id IN ? AND has_update = ?", imageIDs, true).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count pending image updates: %w", err)
	}

	return int(count), nil
}

func (s *DashboardService) getActionableVulnerabilitiesCountInternal(ctx context.Context) (int, error) {
	if s.vulnerabilityService == nil {
		return 0, nil
	}

	summary, err := s.vulnerabilityService.GetEnvironmentSummary(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load vulnerability summary: %w", err)
	}

	if summary == nil || summary.Summary == nil {
		return 0, nil
	}

	return summary.Summary.Critical + summary.Summary.High, nil
}

func (s *DashboardService) getExpiringAPIKeysCountInternal(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.ApiKey{}).
		Where("expires_at IS NOT NULL").
		Where("expires_at <= ?", time.Now().Add(defaultDashboardAPIKeyExpiryWindow)).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count expiring API keys: %w", err)
	}

	return int(count), nil
}
