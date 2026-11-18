package projects

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

func TestProjectJobs(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"db": {
				Name:  "db",
				Image: "postgres:17",
			},
		},
		Jobs: composetypes.Jobs{
			"migrate": {
				Name:      "migrate",
				Image:     "demo/migrate:1",
				DependsOn: composetypes.DependsOnConfig{"seed": {Condition: composetypes.ServiceConditionCompletedSuccessfully}},
			},
			"seed": {
				Name:      "seed",
				Image:     "demo/seed:1",
				DependsOn: composetypes.DependsOnConfig{"db": {Condition: composetypes.ServiceConditionStarted}},
			},
			"backup": {
				Name:  "backup",
				Image: "demo/backup:1",
				Triggers: &composetypes.TriggerConfig{
					Manual: new(false),
					Schedule: []composetypes.ScheduleConfig{
						{
							Cron:     "0 3 * * *",
							Timezone: "Europe/Paris",
						},
					},
				},
			},
		},
		DisabledJobs: composetypes.Jobs{
			"report": {
				Name:     "report",
				Image:    "demo/report:1",
				Profiles: []string{"reports"},
			},
		},
	}

	assert.Equal(t, []types.ProjectJob{
		{
			Name:  "backup",
			Image: "demo/backup:1",
			Schedules: []types.ProjectJobSchedule{
				{
					Cron:     "0 3 * * *",
					Timezone: "Europe/Paris",
				},
			},
		},
		{
			Name:          "migrate",
			Image:         "demo/migrate:1",
			ManualAllowed: true,
		},
		{
			Name:          "report",
			Image:         "demo/report:1",
			Profiles:      []string{"reports"},
			ManualAllowed: true,
			Disabled:      true,
		},
		{
			Name:          "seed",
			Image:         "demo/seed:1",
			ManualAllowed: true,
		},
	}, ProjectJobs(project))

	materialized, err := MaterializeJob(project, "migrate")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"db", "seed", "migrate"}, materialized.ServiceNames())
	assert.Equal(t, "migrate", materialized.Services["migrate"].CustomLabels[api.ServiceLabel])
	assert.NotContains(t, project.Services, "migrate")

	enabled, err := MaterializeJob(project, "report")
	require.NoError(t, err)
	assert.Contains(t, enabled.Profiles, "reports")
	assert.Contains(t, enabled.Services, "report")

	_, err = MaterializeJob(project, "backup")
	require.ErrorIs(t, err, common.ErrValidation)

	_, err = MaterializeJob(project, "missing")
	require.ErrorIs(t, err, common.ErrNotFound)
}
