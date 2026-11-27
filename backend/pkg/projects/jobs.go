package projects

import (
	"fmt"
	"maps"
	"slices"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	mobyContainer "github.com/moby/moby/api/types/container"

	kit "kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

const (
	JobRunLabel = "com.docker.job.id"
)

// ContainerServices returns the Compose service names of the service containers, de-duplicated in first-seen order.
func ContainerServices(containers []mobyContainer.Summary) []string {
	var services []string
	for _, c := range containers {
		service := docker.ComposeServiceLabel(c.Labels)
		if service == "" || c.Labels[api.HookLabel] != "" || c.Labels[JobRunLabel] != "" || c.Labels[api.OneoffLabel] == "True" {
			continue
		}
		if !slices.Contains(services, service) {
			services = append(services, service)
		}
	}
	return services
}

func ProjectJobs(project *composeTypes.Project) []types.ProjectJob {
	if project == nil {
		return nil
	}
	all := project.AllJobs()
	jobs := make([]types.ProjectJob, 0, len(all))
	for _, name := range slices.Sorted(maps.Keys(all)) {
		job := all[name]
		var schedules []types.ProjectJobSchedule
		for _, schedule := range kit.FromPtr(job.Triggers).Schedule {
			schedules = append(schedules, types.ProjectJobSchedule{
				Cron:     schedule.Cron,
				Timezone: schedule.Timezone,
			})
		}
		_, enabled := project.Jobs[name]
		jobs = append(jobs, types.ProjectJob{
			Name:          name,
			Image:         job.Image,
			Profiles:      job.Profiles,
			Schedules:     schedules,
			ManualAllowed: !compose.ManualTriggerDisabled(job),
			Disabled:      !enabled,
			HasBuild:      job.Build != nil,
		})
	}
	return jobs
}

func MaterializeJob(project *composeTypes.Project, name string) (*composeTypes.Project, error) {
	all := project.AllJobs()
	job, ok := all[name]
	if !ok {
		return nil, common.Classify(common.ErrNotFound, fmt.Errorf("no such job: %s", name))
	}
	if _, shadowed := project.AllServices()[name]; shadowed {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("job %q shares its name with a service; rename one of them to run the job", name))
	}
	if compose.ManualTriggerDisabled(job) {
		return nil, common.Classify(common.ErrValidation, compose.ManualTriggerDisabledErr(name))
	}
	selected, err := project.WithSelectedJob(name)
	if err != nil {
		return nil, err
	}
	pending := []string{name}
	seen := map[string]bool{name: true}
	for len(pending) > 0 {
		current := all[pending[0]]
		pending = pending[1:]
		for dep := range current.DependsOn {
			depJob, isJob := all[dep]
			if seen[dep] || !isJob {
				continue
			}
			seen[dep] = true
			if compose.ManualTriggerDisabled(depJob) {
				return nil, common.Classify(common.ErrValidation, fmt.Errorf("job %q is declared with manual: false, it cannot be triggered even as a dependency of another job", dep))
			}
			selected.Services[dep] = compose.JobAsService(selected, dep, depJob)
			pending = append(pending, dep)
		}
	}
	selected.Services[name] = compose.JobAsService(selected, name, job)
	return selected, nil
}
