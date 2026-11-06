package projects

import (
	"context"
	"log/slog"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	composeapi "github.com/docker/compose/v5/pkg/api"

	"kit.getarcane.app"
)

type (
	ImagePullMode int

	ImagePullStep struct {
		Mode         ImagePullMode
		RefreshAfter time.Duration
	}

	DeployImageDecision struct {
		StaleAfter              time.Duration
		Build                   bool
		PullAlways              bool
		PullIfMissing           bool
		PullIfStale             bool
		FallbackBuildOnPullFail bool
		RequireLocalOnly        bool
	}
)

const (
	ImagePullModeNever ImagePullMode = iota
	ImagePullModeIfMissing
	ImagePullModeRefresh
	ImagePullModeAlways
)

func ResolveServiceImagePullMode(svc composetypes.ServiceConfig) ImagePullStep {
	rawPolicy := strings.ToLower(strings.TrimSpace(svc.PullPolicy))
	switch rawPolicy {
	case composetypes.PullPolicyNever:
		return ImagePullStep{Mode: ImagePullModeNever}
	case composetypes.PullPolicyAlways:
		return ImagePullStep{Mode: ImagePullModeAlways}
	case composetypes.PullPolicyMissing,
		composetypes.PullPolicyIfNotPresent,
		composetypes.PullPolicyBuild,
		"":
		return ImagePullStep{Mode: ImagePullModeIfMissing}
	}

	normalized := svc
	normalized.PullPolicy = rawPolicy
	policy, refreshAfter, err := normalized.GetPullPolicy()
	if err != nil {
		slog.WarnContext(context.Background(), "failed to parse service pull_policy, defaulting to missing", //nolint:forbidigo // Pull policy parsing is a pure helper without a request context.
			"service", svc.Name, "pullPolicy", svc.PullPolicy, "error", err)
		return ImagePullStep{Mode: ImagePullModeIfMissing}
	}
	switch policy {
	case composetypes.PullPolicyNever:
		return ImagePullStep{Mode: ImagePullModeNever}
	case composetypes.PullPolicyAlways:
		return ImagePullStep{Mode: ImagePullModeAlways}
	case composetypes.PullPolicyRefresh:
		return ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: refreshAfter}
	default:
		return ImagePullStep{Mode: ImagePullModeIfMissing}
	}
}

func BuildImagePullPlan(project *composetypes.Project) map[string]ImagePullStep {
	plan := map[string]ImagePullStep{}
	record := func(img string, step ImagePullStep) {
		img = strings.TrimSpace(img)
		if img == "" {
			return
		}
		existing, exists := plan[img]
		// A higher mode wins; between two refresh policies the shorter window does.
		moreEager := kit.Ternary(
			step.Mode != existing.Mode,
			step.Mode > existing.Mode,
			step.Mode == ImagePullModeRefresh && step.RefreshAfter < existing.RefreshAfter,
		)
		if !exists || moreEager {
			plan[img] = step
		}
	}
	for _, svc := range project.Services {
		step := ResolveServiceImagePullMode(svc)
		if svc.Build == nil {
			record(svc.Image, step)
		}
		// pre_start hook images inherit the parent service's policy, except that
		// they can never be built: `build` still means pull-if-missing here.
		if step.Mode != ImagePullModeNever {
			for _, img := range composeapi.GetDependentImages(svc, project.Name) {
				record(img, step)
			}
		}
		for _, vol := range svc.Volumes {
			if vol.Type == composetypes.VolumeTypeImage {
				record(vol.Source, ImagePullStep{Mode: ImagePullModeIfMissing})
			}
		}
	}
	return plan
}

func NormalizePullPolicy(policy string) string {
	policy = strings.ToLower(strings.TrimSpace(policy))
	return kit.Ternary(policy == "if_not_present", "missing", policy)
}

func NormalizeDeployPullPolicy(policy string) string {
	normalized := NormalizePullPolicy(policy)
	switch normalized {
	case "always", "missing", "never":
		return normalized
	default:
		return ""
	}
}

func refreshWindowForPolicy(svc composetypes.ServiceConfig, policy string) (time.Duration, bool) {
	normalized := svc
	normalized.PullPolicy = policy
	parsed, window, err := normalized.GetPullPolicy()
	if err != nil || parsed != composetypes.PullPolicyRefresh {
		return 0, false
	}
	return window, true
}

func DecideDeployImageAction(svc composetypes.ServiceConfig, pullPolicyOverride string) DeployImageDecision {
	policy := NormalizePullPolicy(svc.PullPolicy)
	if policy == "" {
		if override := NormalizeDeployPullPolicy(pullPolicyOverride); override != "" {
			policy = override
		}
	}
	buildEnabled := svc.Build != nil

	if buildEnabled {
		switch policy {
		case "build":
			return DeployImageDecision{Build: true}
		case "never":
			return DeployImageDecision{RequireLocalOnly: true}
		case "always":
			return DeployImageDecision{PullAlways: true, FallbackBuildOnPullFail: true}
		default:
			if window, ok := refreshWindowForPolicy(svc, policy); ok {
				return DeployImageDecision{
					PullIfStale:             true,
					StaleAfter:              window,
					FallbackBuildOnPullFail: true,
				}
			}
			return DeployImageDecision{PullIfMissing: true, FallbackBuildOnPullFail: true}
		}
	}

	switch policy {
	case "never":
		return DeployImageDecision{RequireLocalOnly: true}
	case "always":
		return DeployImageDecision{PullAlways: true}
	default:
		if window, ok := refreshWindowForPolicy(svc, policy); ok {
			return DeployImageDecision{PullIfStale: true, StaleAfter: window}
		}
		return DeployImageDecision{PullIfMissing: true}
	}
}
