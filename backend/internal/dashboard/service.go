package dashboard

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
)

const dashboardInventoryCollectTimeout = 15 * time.Second

type DashboardService struct {
	dockerService  *dockerInternal.DockerClientService
	projectService *project.ProjectService
	volumeService  *volume.VolumeService
	podmanService  *podman.Service
}

func NewDashboardService(
	dockerService *dockerInternal.DockerClientService,
	projectService *project.ProjectService,
	volumeService *volume.VolumeService,
	podmanService *podman.Service,
) *DashboardService {
	return &DashboardService{
		dockerService:  dockerService,
		projectService: projectService,
		volumeService:  volumeService,
		podmanService:  podmanService,
	}
}

type inventoryGauges struct {
	containers, health, images, imageSize, volumes, networks, projects metric.Int64ObservableGauge
	swarmNodes, swarmServices, swarmTasks                              metric.Int64ObservableGauge
	podmanPods                                                         metric.Int64ObservableGauge
}

// ObserveInventory reports the local Docker engine's inventory through arcane.docker.* and arcane.projects gauges.
func (s *DashboardService) ObserveInventory(meter metric.Meter) error {
	var g inventoryGauges
	var errs [11]error
	g.containers, errs[0] = meter.Int64ObservableGauge("arcane.docker.containers",
		metric.WithDescription("Containers on the local Docker engine by state"), metric.WithUnit("{container}"))
	g.health, errs[1] = meter.Int64ObservableGauge("arcane.docker.containers.health",
		metric.WithDescription("Containers with a healthcheck by health status"), metric.WithUnit("{container}"))
	g.images, errs[2] = meter.Int64ObservableGauge("arcane.docker.images",
		metric.WithDescription("Images on the local Docker engine by usage"), metric.WithUnit("{image}"))
	g.imageSize, errs[3] = meter.Int64ObservableGauge("arcane.docker.images.size",
		metric.WithDescription("Total size of images on the local Docker engine"), metric.WithUnit("By"))
	g.volumes, errs[4] = meter.Int64ObservableGauge("arcane.docker.volumes",
		metric.WithDescription("Volumes on the local Docker engine by usage, excluding Arcane-internal volumes"), metric.WithUnit("{volume}"))
	g.networks, errs[5] = meter.Int64ObservableGauge("arcane.docker.networks",
		metric.WithDescription("Networks on the local Docker engine by usage; built-in networks report as default"), metric.WithUnit("{network}"))
	g.projects, errs[6] = meter.Int64ObservableGauge("arcane.projects",
		metric.WithDescription("Arcane Compose projects by status"), metric.WithUnit("{project}"))
	g.swarmNodes, errs[7] = meter.Int64ObservableGauge("arcane.docker.swarm.nodes",
		metric.WithDescription("Swarm nodes by role and state, reported by swarm managers"), metric.WithUnit("{node}"))
	g.swarmServices, errs[8] = meter.Int64ObservableGauge("arcane.docker.swarm.services",
		metric.WithDescription("Swarm services, reported by swarm managers"), metric.WithUnit("{service}"))
	g.swarmTasks, errs[9] = meter.Int64ObservableGauge("arcane.docker.swarm.service.tasks",
		metric.WithDescription("Running and desired tasks per swarm service, reported by swarm managers"), metric.WithUnit("{task}"))
	g.podmanPods, errs[10] = meter.Int64ObservableGauge("arcane.podman.pods",
		metric.WithDescription("Podman pods on the local engine, reported when Podman is detected"), metric.WithUnit("{pod}"))
	if err := errors.Join(errs[:]...); err != nil {
		return err
	}

	_, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, dashboardInventoryCollectTimeout)
		defer cancel()
		snapshot, err := s.dockerService.GetSnapshot(ctx, environment.LocalEnvironmentID)
		if err != nil {
			slog.DebugContext(ctx, "Skipping Docker inventory metrics", "error", err)
			return nil
		}

		states := map[dockercontainer.ContainerState]int64{
			dockercontainer.StateCreated: 0, dockercontainer.StateRunning: 0, dockercontainer.StatePaused: 0, dockercontainer.StateRestarting: 0,
			dockercontainer.StateRemoving: 0, dockercontainer.StateExited: 0, dockercontainer.StateDead: 0,
		}
		health := map[dockercontainer.HealthStatus]int64{dockercontainer.Starting: 0, dockercontainer.Healthy: 0, dockercontainer.Unhealthy: 0}
		for _, c := range snapshot.Containers {
			states[c.State]++
			if c.Health != nil && c.Health.Status != dockercontainer.NoHealthcheck {
				health[c.Health.Status]++
			}
		}
		for state, n := range states {
			o.ObserveInt64(g.containers, n, metric.WithAttributes(attribute.String("container.state", string(state))))
		}
		for status, n := range health {
			o.ObserveInt64(g.health, n, metric.WithAttributes(attribute.String("container.health.status", string(status))))
		}

		images := dockerInternal.CountImageUsage(snapshot.Images, container.FilterExcludedContainers(snapshot.Containers, false, true))
		o.ObserveInt64(g.images, int64(images.Inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
		o.ObserveInt64(g.images, int64(images.Unused), metric.WithAttributes(attribute.String("usage", "unused")))
		o.ObserveInt64(g.imageSize, images.TotalSize)

		if s.volumeService != nil && snapshot.Volumes != nil {
			volumes := s.volumeService.CountUsageFromSnapshot(snapshot.Volumes.Items, snapshot.Containers)
			o.ObserveInt64(g.volumes, int64(volumes.Inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
			o.ObserveInt64(g.volumes, int64(volumes.Unused), metric.WithAttributes(attribute.String("usage", "unused")))
		}

		if len(snapshot.Networks) > 0 {
			inuse, unused, total := dockerInternal.CountNetworkUsage(snapshot.Networks, snapshot.Containers)
			o.ObserveInt64(g.networks, int64(inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
			o.ObserveInt64(g.networks, int64(unused), metric.WithAttributes(attribute.String("usage", "unused")))
			o.ObserveInt64(g.networks, int64(total-inuse-unused), metric.WithAttributes(attribute.String("usage", "default")))
		}

		if s.projectService != nil {
			if counts, countErr := s.projectService.GetProjectStatusCounts(ctx); countErr != nil {
				slog.DebugContext(ctx, "Skipping project inventory metrics", "error", countErr)
			} else {
				other := counts.TotalProjects - counts.RunningProjects - counts.StoppedProjects - counts.ArchivedProjects
				for status, n := range map[string]int{"running": counts.RunningProjects, "stopped": counts.StoppedProjects, "archived": counts.ArchivedProjects, "unknown": other} {
					o.ObserveInt64(g.projects, int64(n), metric.WithAttributes(attribute.String("project.status", status)))
				}
			}
		}

		s.observeSwarm(ctx, o, &g)
		if pods, detected := s.podmanService.PodCount(ctx).Get(); detected {
			o.ObserveInt64(g.podmanPods, int64(pods))
		}
		return nil
	}, g.containers, g.health, g.images, g.imageSize, g.volumes, g.networks, g.projects, g.swarmNodes, g.swarmServices, g.swarmTasks, g.podmanPods)
	return err
}

// observeSwarm reports nodes and services only when the local engine is a swarm manager; the ping keeps non-swarm hosts cheap.
func (s *DashboardService) observeSwarm(ctx context.Context, o metric.Observer, g *inventoryGauges) {
	ping, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.PingResult, error) {
		return cli.Ping(ctx, client.PingOptions{})
	})
	if err != nil || ping.SwarmStatus == nil || !ping.SwarmStatus.ControlAvailable {
		return
	}

	if nodes, nodeErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.NodeListResult, error) {
		return cli.NodeList(ctx, client.NodeListOptions{})
	}); nodeErr != nil {
		slog.DebugContext(ctx, "Skipping swarm node metrics", "error", nodeErr)
	} else {
		counts := map[[2]string]int64{}
		for _, node := range nodes.Items {
			counts[[2]string{string(node.Spec.Role), string(node.Status.State)}]++
		}
		for key, n := range counts {
			o.ObserveInt64(g.swarmNodes, n, metric.WithAttributes(attribute.String("swarm.node.role", key[0]), attribute.String("swarm.node.state", key[1])))
		}
	}

	services, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ServiceListResult, error) {
		return cli.ServiceList(ctx, client.ServiceListOptions{Status: true})
	})
	if err != nil {
		slog.DebugContext(ctx, "Skipping swarm service metrics", "error", err)
		return
	}
	o.ObserveInt64(g.swarmServices, int64(len(services.Items)))
	for _, service := range services.Items {
		status := cmp.Or(service.ServiceStatus, &swarm.ServiceStatus{})
		name := attribute.String("swarm.service.name", service.Spec.Name)
		o.ObserveInt64(g.swarmTasks, int64(status.RunningTasks), metric.WithAttributes(name, attribute.String("swarm.task.state", "running")))
		o.ObserveInt64(g.swarmTasks, int64(status.DesiredTasks), metric.WithAttributes(name, attribute.String("swarm.task.state", "desired")))
	}
}
