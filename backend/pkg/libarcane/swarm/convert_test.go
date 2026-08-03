package swarm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/backend/v2/pkg/projects/types"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/require"
)

func TestConvertServiceMountsResolvesComposeResourceNames(t *testing.T) {
	mounts, err := convertServiceMountsInternal(
		[]composegotypes.ServiceVolumeConfig{
			{Type: "volume", Source: "plain", Target: "/plain"},
			{Type: "volume", Source: "driver", Target: "/driver"},
			{Type: "volume", Source: "named", Target: "/named"},
			{Type: "volume", Source: "external", Target: "/external"},
		},
		namespace{name: "stack"},
		composegotypes.Volumes{
			"plain":    {Name: "stack_plain"},
			"driver":   {Name: "stack_driver", Driver: "local"},
			"named":    {Name: "custom"},
			"external": {Name: "external", External: true},
		},
		map[string]string{swarmtypes.StackNamespaceLabel: "stack"},
	)
	require.NoError(t, err)
	require.Len(t, mounts, 4)
	require.Equal(t, mount.TypeVolume, mounts[0].Type)
	require.Equal(t, "stack_plain", mounts[0].Source)
	require.NotNil(t, mounts[0].VolumeOptions)
	require.NotNil(t, mounts[0].VolumeOptions.DriverConfig)
	require.Equal(t, "stack_driver", mounts[1].Source)
	require.Equal(t, "local", mounts[1].VolumeOptions.DriverConfig.Name)
	require.Equal(t, "custom", mounts[2].Source)
	require.Equal(t, "external", mounts[3].Source)
	require.NotNil(t, mounts[3].VolumeOptions)
	require.Nil(t, mounts[3].VolumeOptions.DriverConfig)
}

func TestApplyDeployConfigConvertsCPUFractionToNanoCPUs(t *testing.T) {
	spec := &swarm.ServiceSpec{}
	deploy := &composegotypes.DeployConfig{
		Resources: composegotypes.Resources{
			Limits: &composegotypes.Resource{
				NanoCPUs:    0.5,
				MemoryBytes: 536870912,
			},
			Reservations: &composegotypes.Resource{
				NanoCPUs:    0.25,
				MemoryBytes: 268435456,
			},
		},
	}

	require.NoError(t, applyDeployConfigInternal(spec, deploy, nil, ""))
	require.Equal(t, int64(500_000_000), spec.TaskTemplate.Resources.Limits.NanoCPUs)
	require.Equal(t, int64(536870912), spec.TaskTemplate.Resources.Limits.MemoryBytes)
	require.Equal(t, int64(250_000_000), spec.TaskTemplate.Resources.Reservations.NanoCPUs)
	require.Equal(t, int64(268435456), spec.TaskTemplate.Resources.Reservations.MemoryBytes)
}

func TestPlanConfigsUsesComposeProjectEnvironment(t *testing.T) {
	t.Setenv("ARCANE_SWARM_CONFIG_SOURCE", "process-value")

	project, err := projects.LoadComposeProjectFromContent(context.Background(), projecttypes.ComposeContentOptions{
		ProjectName: "stack",
		ComposeContent: `
services:
  app:
    image: alpine
    configs:
      - source: app_config
configs:
  app_config:
    environment: ARCANE_SWARM_CONFIG_SOURCE
`,
		EnvContent: "ARCANE_SWARM_CONFIG_SOURCE=\n",
	})
	require.NoError(t, err)

	plans, err := planConfigsInternal(project, namespace{name: "stack"})
	require.NoError(t, err)
	require.Empty(t, plans["app_config"].Data)
	require.Equal(t, managedResourceNameInternal("stack_app_config", hashManagedResourceInternal(nil)), plans["app_config"].Meta.Name)
}

func TestBuildServiceSpecConformance(t *testing.T) {
	type testCase struct {
		name      string
		configure func(*composegotypes.ServiceConfig)
		check     func(*testing.T, swarm.ServiceSpec, error)
	}

	tests := []testCase{
		{
			name: "extra hosts use swarm notation",
			configure: func(service *composegotypes.ServiceConfig) {
				service.ExtraHosts = composegotypes.HostsList{"api.local": {"127.0.0.1"}}
			},
			check: func(t *testing.T, spec swarm.ServiceSpec, err error) {
				require.NoError(t, err)
				require.Equal(t, []string{"127.0.0.1 api.local"}, spec.TaskTemplate.ContainerSpec.Hosts)
			},
		},
		{
			name: "network has service alias",
			configure: func(service *composegotypes.ServiceConfig) {
				service.Networks = map[string]*composegotypes.ServiceNetworkConfig{"default": nil}
			},
			check: func(t *testing.T, spec swarm.ServiceSpec, err error) {
				require.NoError(t, err)
				require.Equal(t, []string{"web"}, spec.TaskTemplate.Networks[0].Aliases)
			},
		},
		{
			name: "update parallelism defaults to one",
			configure: func(service *composegotypes.ServiceConfig) {
				service.Deploy = &composegotypes.DeployConfig{UpdateConfig: &composegotypes.UpdateConfig{}}
			},
			check: func(t *testing.T, spec swarm.ServiceSpec, err error) {
				require.NoError(t, err)
				require.Equal(t, uint64(1), spec.UpdateConfig.Parallelism)
			},
		},
		{
			name: "legacy restart policy is converted",
			configure: func(service *composegotypes.ServiceConfig) {
				service.Restart = "on-failure:3"
			},
			check: func(t *testing.T, spec swarm.ServiceSpec, err error) {
				require.NoError(t, err)
				require.Equal(t, swarm.RestartPolicyConditionOnFailure, spec.TaskTemplate.RestartPolicy.Condition)
				require.Equal(t, uint64(3), *spec.TaskTemplate.RestartPolicy.MaxAttempts)
			},
		},
		{
			name: "disabled healthcheck uses none",
			configure: func(service *composegotypes.ServiceConfig) {
				service.HealthCheck = &composegotypes.HealthCheckConfig{Disable: true}
			},
			check: func(t *testing.T, spec swarm.ServiceSpec, err error) {
				require.NoError(t, err)
				require.Equal(t, []string{"NONE"}, spec.TaskTemplate.ContainerSpec.Healthcheck.Test)
			},
		},
		{
			name: "disabled healthcheck rejects other fields",
			configure: func(service *composegotypes.ServiceConfig) {
				retries := uint64(1)
				service.HealthCheck = &composegotypes.HealthCheckConfig{Disable: true, Retries: &retries}
			},
			check: func(t *testing.T, _ swarm.ServiceSpec, err error) {
				require.ErrorContains(t, err, "cannot be combined")
			},
		},
		{
			name: "global mode rejects replicas",
			configure: func(service *composegotypes.ServiceConfig) {
				replicas := 2
				service.Deploy = &composegotypes.DeployConfig{Mode: "global", Replicas: &replicas}
			},
			check: func(t *testing.T, _ swarm.ServiceSpec, err error) {
				require.ErrorContains(t, err, "replicas can only be used")
			},
		},
		{
			name: "invalid published port fails",
			configure: func(service *composegotypes.ServiceConfig) {
				service.Ports = []composegotypes.ServicePortConfig{{Target: 80, Published: "invalid"}}
			},
			check: func(t *testing.T, _ swarm.ServiceSpec, err error) {
				require.ErrorContains(t, err, "invalid published port")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := composegotypes.ServiceConfig{Name: "web", Image: "nginx:alpine"}
			test.configure(&service)
			spec, err := buildServiceSpecInternal(
				service,
				namespace{name: "stack"},
				map[string]string{swarmtypes.StackNamespaceLabel: "stack"},
				map[string]string{"default": "stack_default"},
				nil,
				nil,
				nil,
			)
			test.check(t, spec, err)
		})
	}
}

func TestStackConversionEndToEnd(t *testing.T) {
	workingDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "db_pw.txt"), []byte("secret"), 0o600))

	project, err := projects.LoadComposeProjectFromContent(context.Background(), projecttypes.ComposeContentOptions{
		ProjectName: "stack",
		ComposeContent: `
services:
  web:
    image: nginx:alpine
    extra_hosts:
      api.local: 127.0.0.1
    volumes:
      - data:/var/lib/app
    networks:
      default:
        aliases:
          - frontend
    configs:
      - source: app_config
    secrets:
      - source: db_pw
    deploy:
      update_config: {}
volumes:
  data: {}
networks:
  default:
    ipam:
      options:
        source: fixture
configs:
  app_config:
    content: enabled=true
secrets:
  db_pw:
    file: db_pw.txt
`,
		WorkingDir: workingDir,
	})
	require.NoError(t, err)

	ns := namespace{name: "stack"}
	networkNames := planNetworksInternal(project, ns)
	volumeNames := planVolumesInternal(project, ns)
	configPlans, err := planConfigsInternal(project, ns)
	require.NoError(t, err)
	secretPlans, err := planSecretsInternal(project, ns)
	require.NoError(t, err)
	configMeta := make(map[string]resourceMeta, len(configPlans))
	for key, plan := range configPlans {
		configMeta[key] = resourceMeta{ID: "config-id", Name: plan.Meta.Name}
	}
	secretMeta := make(map[string]resourceMeta, len(secretPlans))
	for key, plan := range secretPlans {
		secretMeta[key] = resourceMeta{ID: "secret-id", Name: plan.Meta.Name}
	}
	stackLabels := map[string]string{swarmtypes.StackNamespaceLabel: "stack"}

	build := func() swarm.ServiceSpec {
		spec, buildErr := buildServiceSpecInternal(
			project.Services["web"],
			ns,
			stackLabels,
			networkNames,
			configMeta,
			secretMeta,
			project.Volumes,
		)
		require.NoError(t, buildErr)
		return spec
	}
	spec := build()

	checks := []struct {
		aspect string
		check  func(*testing.T)
	}{
		{"names", func(t *testing.T) {
			require.Equal(t, "stack_web", spec.Name)
			require.Equal(t, "stack_default", networkNames["default"])
			require.Equal(t, "stack_data", volumeNames["data"])
		}},
		{"extra hosts", func(t *testing.T) {
			require.Equal(t, []string{"127.0.0.1 api.local"}, spec.TaskTemplate.ContainerSpec.Hosts)
		}},
		{"network alias", func(t *testing.T) {
			require.Contains(t, spec.TaskTemplate.Networks[0].Aliases, "web")
		}},
		{"volume options", func(t *testing.T) {
			volumeMount := spec.TaskTemplate.ContainerSpec.Mounts[0]
			require.Equal(t, "stack_data", volumeMount.Source)
			require.Equal(t, "stack", volumeMount.VolumeOptions.Labels[swarmtypes.StackNamespaceLabel])
			require.NotNil(t, volumeMount.VolumeOptions.DriverConfig)
		}},
		{"file targets", func(t *testing.T) {
			require.Equal(t, "app_config", spec.TaskTemplate.ContainerSpec.Configs[0].File.Name)
			require.Equal(t, "/run/secrets/db_pw", spec.TaskTemplate.ContainerSpec.Secrets[0].File.Name)
		}},
		{"update defaults", func(t *testing.T) {
			require.Equal(t, uint64(1), spec.UpdateConfig.Parallelism)
		}},
		{"ipam options", func(t *testing.T) {
			require.Equal(t, "fixture", convertIPAMInternal(project.Networks["default"].Ipam).Options["source"])
		}},
		{"determinism", func(t *testing.T) {
			require.Equal(t, spec, build())
		}},
	}

	for _, check := range checks {
		t.Run(check.aspect, check.check)
	}
}
