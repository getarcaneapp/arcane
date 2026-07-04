package projects

import (
	"log/slog"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// ImagePullMode describes when Arcane should pull an image for a project.
type ImagePullMode int

const (
	// ImagePullModeNever skips pulling the image.
	ImagePullModeNever ImagePullMode = iota
	// ImagePullModeIfMissing pulls only when the image is missing locally.
	ImagePullModeIfMissing
	// ImagePullModeAlways pulls even when the image is present locally.
	ImagePullModeAlways
)

// DeployImageDecision describes how deploy should handle a service image.
type DeployImageDecision struct {
	Build                   bool
	PullAlways              bool
	PullIfMissing           bool
	FallbackBuildOnPullFail bool
	RequireLocalOnly        bool
}

// ResolveServiceImagePullMode resolves compose pull_policy into Arcane's pull mode.
func ResolveServiceImagePullMode(svc composetypes.ServiceConfig) ImagePullMode {
	rawPolicy := strings.ToLower(strings.TrimSpace(svc.PullPolicy))
	switch {
	case rawPolicy == composetypes.PullPolicyNever:
		return ImagePullModeNever
	case rawPolicy == composetypes.PullPolicyAlways:
		return ImagePullModeAlways
	case rawPolicy == composetypes.PullPolicyRefresh,
		rawPolicy == "daily",
		rawPolicy == "weekly",
		strings.HasPrefix(rawPolicy, "every_"):
		return ImagePullModeAlways
	case rawPolicy == composetypes.PullPolicyMissing,
		rawPolicy == composetypes.PullPolicyIfNotPresent,
		rawPolicy == composetypes.PullPolicyBuild,
		rawPolicy == "":
		return ImagePullModeIfMissing
	}

	policy, _, err := svc.GetPullPolicy()
	if err != nil {
		slog.Warn("failed to parse service pull_policy, defaulting to missing", "service", svc.Name, "pull_policy", svc.PullPolicy, "error", err)
		return ImagePullModeIfMissing
	}

	switch policy {
	case composetypes.PullPolicyNever:
		return ImagePullModeNever
	case composetypes.PullPolicyAlways, composetypes.PullPolicyRefresh:
		return ImagePullModeAlways
	case composetypes.PullPolicyMissing, composetypes.PullPolicyIfNotPresent, composetypes.PullPolicyBuild:
		return ImagePullModeIfMissing
	default:
		return ImagePullModeIfMissing
	}
}

// BuildImagePullPlan builds a deduplicated image pull plan for non-build services.
func BuildImagePullPlan(services composetypes.Services) map[string]ImagePullMode {
	plan := map[string]ImagePullMode{}
	for _, svc := range services {
		if svc.Build != nil {
			continue
		}
		img := strings.TrimSpace(svc.Image)
		if img == "" {
			continue
		}
		mode := ResolveServiceImagePullMode(svc)
		if existing, exists := plan[img]; !exists || mode > existing {
			plan[img] = mode
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

// IsAlwaysPullPolicy reports whether policy means always pull.
func IsAlwaysPullPolicy(policy string) bool {
	if policy == "always" || policy == "daily" || policy == "weekly" {
		return true
	}
	return strings.HasPrefix(policy, "every_")
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
			return DeployImageDecision{PullIfMissing: true}
		}
	}

	switch {
	case policy == "never":
		return DeployImageDecision{RequireLocalOnly: true}
	case IsAlwaysPullPolicy(policy):
		return DeployImageDecision{PullAlways: true}
	default:
		return DeployImageDecision{PullIfMissing: true}
	}
}
