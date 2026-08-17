package projects

import (
	"log/slog"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	composeapi "github.com/docker/compose/v5/pkg/api"
)

// ImagePullMode describes when Arcane should pull an image for a project.
type ImagePullMode int

const (
	// ImagePullModeNever skips pulling the image.
	ImagePullModeNever ImagePullMode = iota
	// ImagePullModeIfMissing pulls only when the image is missing locally.
	ImagePullModeIfMissing
	// ImagePullModeRefresh pulls when the image is missing locally or was last
	// tagged longer ago than the policy's refresh window.
	ImagePullModeRefresh
	// ImagePullModeAlways pulls even when the image is present locally.
	ImagePullModeAlways
)

// ImagePullStep is one entry of an image pull plan: the mode plus, for
// ImagePullModeRefresh, the window after which a new pull is due.
type ImagePullStep struct {
	Mode         ImagePullMode
	RefreshAfter time.Duration
}

// DeployImageDecision describes how deploy should handle a service image.
type DeployImageDecision struct {
	Build         bool
	PullAlways    bool
	PullIfMissing bool
	// PullIfStale pulls when the image is missing or its last-tag time is
	// older than StaleAfter (pull_policy daily/weekly/every_N).
	PullIfStale             bool
	StaleAfter              time.Duration
	FallbackBuildOnPullFail bool
	RequireLocalOnly        bool
}

// ResolveServiceImagePullMode resolves compose pull_policy into Arcane's pull step.
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

	// Refresh windows (daily, weekly, every_<duration>) and anything else:
	// compose-go owns the parsing, matching compose's own `up` semantics —
	// unknown or unparseable policies fall back to pull-if-missing.
	normalized := svc
	normalized.PullPolicy = rawPolicy
	policy, refreshAfter, err := normalized.GetPullPolicy()
	if err != nil {
		slog.Warn("failed to parse service pull_policy, defaulting to missing", "service", svc.Name, "pull_policy", svc.PullPolicy, "error", err)
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

// morePullEagerInternal reports whether a beats b in a pull plan: a higher
// mode wins, and between two refresh policies the shorter window does.
func morePullEagerInternal(a, b ImagePullStep) bool {
	if a.Mode != b.Mode {
		return a.Mode > b.Mode
	}
	return a.Mode == ImagePullModeRefresh && a.RefreshAfter < b.RefreshAfter
}

// BuildImagePullPlan builds a deduplicated image pull plan covering non-build
// service images, pre_start hook images, and type:image volume sources.
func BuildImagePullPlan(project *composetypes.Project) map[string]ImagePullStep {
	plan := map[string]ImagePullStep{}
	record := func(img string, step ImagePullStep) {
		img = strings.TrimSpace(img)
		if img == "" {
			return
		}
		if existing, exists := plan[img]; !exists || morePullEagerInternal(step, existing) {
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

// NormalizePullPolicy normalizes compose pull policy aliases.
func NormalizePullPolicy(policy string) string {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "if_not_present" {
		return "missing"
	}
	return policy
}

// NormalizeDeployPullPolicy returns a supported deploy pull policy or empty string.
func NormalizeDeployPullPolicy(policy string) string {
	normalized := NormalizePullPolicy(policy)
	switch normalized {
	case "always", "missing", "never":
		return normalized
	default:
		return ""
	}
}

// IsAlwaysPullPolicy reports whether policy means an unconditional pull.
// Refresh windows (daily/weekly/every_N) are not "always": they pull only
// once their window has elapsed, matching compose v5.5.0.
func IsAlwaysPullPolicy(policy string) bool {
	return policy == "always"
}

// refreshWindowForPolicyInternal resolves policy as a refresh window
// (daily/weekly/every_N), reporting false for every other policy.
func refreshWindowForPolicyInternal(svc composetypes.ServiceConfig, policy string) (time.Duration, bool) {
	normalized := svc
	normalized.PullPolicy = policy
	parsed, window, err := normalized.GetPullPolicy()
	if err != nil || parsed != composetypes.PullPolicyRefresh {
		return 0, false
	}
	return window, true
}

// DecideDeployImageAction decides whether deploy should build, pull, or require local images.
func DecideDeployImageAction(svc composetypes.ServiceConfig, pullPolicyOverride string) DeployImageDecision {
	policy := NormalizePullPolicy(svc.PullPolicy)
	if policy == "" {
		if override := NormalizeDeployPullPolicy(pullPolicyOverride); override != "" {
			policy = override
		}
	}
	buildEnabled := svc.Build != nil

	if buildEnabled {
		switch {
		case policy == "build":
			return DeployImageDecision{Build: true}
		case policy == "never":
			return DeployImageDecision{RequireLocalOnly: true}
		case IsAlwaysPullPolicy(policy):
			return DeployImageDecision{PullAlways: true}
		case policy == "missing":
			return DeployImageDecision{PullIfMissing: true}
		case policy == "":
			return DeployImageDecision{PullIfMissing: true, FallbackBuildOnPullFail: true}
		default:
			if window, ok := refreshWindowForPolicyInternal(svc, policy); ok {
				return DeployImageDecision{PullIfStale: true, StaleAfter: window}
			}
			return DeployImageDecision{PullIfMissing: true}
		}
	}

	switch {
	case policy == "never":
		return DeployImageDecision{RequireLocalOnly: true}
	case IsAlwaysPullPolicy(policy):
		return DeployImageDecision{PullAlways: true}
	default:
		if window, ok := refreshWindowForPolicyInternal(svc, policy); ok {
			return DeployImageDecision{PullIfStale: true, StaleAfter: window}
		}
		return DeployImageDecision{PullIfMissing: true}
	}
}
