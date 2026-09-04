package projects

import (
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
)

func TestResolveServiceImagePullMode(t *testing.T) {
	tests := []struct {
		name     string
		service  composetypes.ServiceConfig
		expected ImagePullStep
	}{
		{
			name:     "default policy is missing",
			service:  composetypes.ServiceConfig{},
			expected: ImagePullStep{Mode: ImagePullModeIfMissing},
		},
		{
			name:     "always policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyAlways},
			expected: ImagePullStep{Mode: ImagePullModeAlways},
		},
		{
			name:     "bare refresh policy has a zero refresh window",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyRefresh},
			expected: ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: 0},
		},
		{
			name:     "daily policy is a 24h refresh window",
			service:  composetypes.ServiceConfig{PullPolicy: "daily"},
			expected: ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: 24 * time.Hour},
		},
		{
			name:     "weekly policy is a 7d refresh window",
			service:  composetypes.ServiceConfig{PullPolicy: "weekly"},
			expected: ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: 7 * 24 * time.Hour},
		},
		{
			name:     "every_12h policy parses its window",
			service:  composetypes.ServiceConfig{PullPolicy: "every_12h"},
			expected: ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: 12 * time.Hour},
		},
		{
			name:     "every with invalid duration defaults to missing behavior",
			service:  composetypes.ServiceConfig{PullPolicy: "every_bogus"},
			expected: ImagePullStep{Mode: ImagePullModeIfMissing},
		},
		{
			name:     "missing policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyMissing},
			expected: ImagePullStep{Mode: ImagePullModeIfMissing},
		},
		{
			name:     "if not present policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyIfNotPresent},
			expected: ImagePullStep{Mode: ImagePullModeIfMissing},
		},
		{
			name:     "never policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyNever},
			expected: ImagePullStep{Mode: ImagePullModeNever},
		},
		{
			name:     "invalid policy defaults to missing behavior",
			service:  composetypes.ServiceConfig{PullPolicy: "definitely_invalid"},
			expected: ImagePullStep{Mode: ImagePullModeIfMissing},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ResolveServiceImagePullMode(tt.service))
		})
	}
}

func TestBuildProjectImagePullPlan(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"web": {
				Name:       "web",
				Image:      "redis:latest",
				PullPolicy: composetypes.PullPolicyIfNotPresent,
			},
			"worker": {
				Name:       "worker",
				Image:      "redis:latest",
				PullPolicy: composetypes.PullPolicyAlways,
			},
			"api": {
				Name:       "api",
				Image:      "nginx:latest",
				PullPolicy: composetypes.PullPolicyNever,
			},
			"empty-image": {
				Name:       "empty-image",
				Image:      "",
				PullPolicy: composetypes.PullPolicyAlways,
			},
		},
	}

	plan := BuildImagePullPlan(project)

	assert.Len(t, plan, 2)
	assert.Equal(t, ImagePullStep{Mode: ImagePullModeAlways}, plan["redis:latest"])
	assert.Equal(t, ImagePullStep{Mode: ImagePullModeNever}, plan["nginx:latest"])
}

func TestBuildImagePullPlanRefreshMerge(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"daily": {
				Name:       "daily",
				Image:      "shared:latest",
				PullPolicy: "daily",
			},
			"hourly": {
				Name:       "hourly",
				Image:      "shared:latest",
				PullPolicy: "every_1h",
			},
			"missing": {
				Name:       "missing",
				Image:      "shared:latest",
				PullPolicy: composetypes.PullPolicyMissing,
			},
			"weekly": {
				Name:       "weekly",
				Image:      "other:latest",
				PullPolicy: "weekly",
			},
			"always": {
				Name:       "always",
				Image:      "other:latest",
				PullPolicy: composetypes.PullPolicyAlways,
			},
		},
	}

	plan := BuildImagePullPlan(project)

	// shortest refresh window wins over longer windows and pull-if-missing
	assert.Equal(t, ImagePullStep{Mode: ImagePullModeRefresh, RefreshAfter: time.Hour}, plan["shared:latest"])
	// always wins over any refresh window
	assert.Equal(t, ImagePullStep{Mode: ImagePullModeAlways}, plan["other:latest"])
}

func TestBuildImagePullPlanDependentImages(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"web": {
				Name:     "web",
				Image:    "nginx:latest",
				PreStart: []composetypes.ServiceHook{{Image: "busybox:stable", Command: composetypes.ShellCommand{"true"}}},
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeImage, Source: "content:latest", Target: "/data"},
				},
			},
			"local": {
				Name:       "local",
				Image:      "local:dev",
				PullPolicy: composetypes.PullPolicyNever,
				PreStart:   []composetypes.ServiceHook{{Image: "skipped:hook", Command: composetypes.ShellCommand{"true"}}},
			},
		},
	}

	plan := BuildImagePullPlan(project)

	assert.Equal(t, ImagePullStep{Mode: ImagePullModeIfMissing}, plan["busybox:stable"])
	assert.Equal(t, ImagePullStep{Mode: ImagePullModeIfMissing}, plan["content:latest"])
	assert.NotContains(t, plan, "skipped:hook")
}

func TestNormalizePullPolicy(t *testing.T) {
	assert.Equal(t, "missing", NormalizePullPolicy("if_not_present"))
	assert.Equal(t, "build", NormalizePullPolicy(" BUILD "))
	assert.Empty(t, NormalizePullPolicy(""))
}

func TestDecideDeployImageAction(t *testing.T) {
	t.Run("build service with explicit build policy", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			PullPolicy: "build",
			Build:      &composetypes.BuildConfig{Context: "."},
		}

		decision := DecideDeployImageAction(svc, "")
		assert.True(t, decision.Build)
		assert.False(t, decision.PullAlways)
	})

	t.Run("build service default policy uses pull then fallback build", func(t *testing.T) {
		svc := composetypes.ServiceConfig{Build: &composetypes.BuildConfig{Context: "."}}
		decision := DecideDeployImageAction(svc, "")
		assert.True(t, decision.PullIfMissing)
		assert.True(t, decision.FallbackBuildOnPullFail)
		assert.False(t, decision.Build)
	})

	t.Run("non-build service never policy requires local only", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "never"}
		decision := DecideDeployImageAction(svc, "")
		assert.True(t, decision.RequireLocalOnly)
		assert.False(t, decision.PullIfMissing)
	})

	t.Run("compose pull policy wins over deploy override", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "never"}
		decision := DecideDeployImageAction(svc, "always")
		assert.True(t, decision.RequireLocalOnly)
		assert.False(t, decision.PullAlways)
	})

	t.Run("refresh policy pulls when stale instead of always", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "daily"}
		decision := DecideDeployImageAction(svc, "")
		assert.True(t, decision.PullIfStale)
		assert.Equal(t, 24*time.Hour, decision.StaleAfter)
		assert.False(t, decision.PullAlways)
	})

	t.Run("refresh policy on build service pulls when stale", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			PullPolicy: "every_6h",
			Build:      &composetypes.BuildConfig{Context: "."},
		}
		decision := DecideDeployImageAction(svc, "")
		assert.True(t, decision.PullIfStale)
		assert.Equal(t, 6*time.Hour, decision.StaleAfter)
		assert.False(t, decision.Build)
	})
}

func TestShouldPullDeployImage(t *testing.T) {
	refresh := DeployImageDecision{PullIfStale: true, StaleAfter: 24 * time.Hour}

	t.Run("always pulls regardless of presence", func(t *testing.T) {
		assert.True(t, ShouldPullDeployImage(DeployImageDecision{PullAlways: true}, true, time.Now()))
	})

	t.Run("missing image pulls under refresh policy", func(t *testing.T) {
		assert.True(t, ShouldPullDeployImage(refresh, false, time.Time{}))
	})

	t.Run("fresh image within window skips pull", func(t *testing.T) {
		assert.False(t, ShouldPullDeployImage(refresh, true, time.Now().Add(-time.Hour)))
	})

	t.Run("image older than window pulls", func(t *testing.T) {
		assert.True(t, ShouldPullDeployImage(refresh, true, time.Now().Add(-25*time.Hour)))
	})

	t.Run("unknown last-tag time counts as stale", func(t *testing.T) {
		assert.True(t, ShouldPullDeployImage(refresh, true, time.Time{}))
	})

	t.Run("pull-if-missing skips present image", func(t *testing.T) {
		assert.False(t, ShouldPullDeployImage(DeployImageDecision{PullIfMissing: true}, true, time.Time{}))
	})
}
