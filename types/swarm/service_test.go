package swarm

import (
	"testing"

	mobyswarm "github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/require"
)

func TestNewServiceSummary_ReportsJobModes(t *testing.T) {
	totalCompletions := uint64(5)
	maxConcurrent := uint64(2)

	tests := []struct {
		name         string
		service      mobyswarm.Service
		wantMode     string
		wantReplicas uint64
	}{
		{
			name: "replicated job uses total completions",
			service: mobyswarm.Service{
				Spec: mobyswarm.ServiceSpec{
					Mode: mobyswarm.ServiceMode{
						ReplicatedJob: &mobyswarm.ReplicatedJob{
							MaxConcurrent:    &maxConcurrent,
							TotalCompletions: &totalCompletions,
						},
					},
				},
			},
			wantMode:     "replicated-job",
			wantReplicas: totalCompletions,
		},
		{
			name: "replicated job falls back to max concurrent",
			service: mobyswarm.Service{
				Spec: mobyswarm.ServiceSpec{
					Mode: mobyswarm.ServiceMode{
						ReplicatedJob: &mobyswarm.ReplicatedJob{MaxConcurrent: &maxConcurrent},
					},
				},
			},
			wantMode:     "replicated-job",
			wantReplicas: maxConcurrent,
		},
		{
			name: "replicated job falls back to Docker default",
			service: mobyswarm.Service{
				Spec: mobyswarm.ServiceSpec{
					Mode: mobyswarm.ServiceMode{
						ReplicatedJob: &mobyswarm.ReplicatedJob{},
					},
				},
			},
			wantMode:     "replicated-job",
			wantReplicas: 1,
		},
		{
			name: "global job uses service status desired tasks",
			service: mobyswarm.Service{
				Spec: mobyswarm.ServiceSpec{
					Mode: mobyswarm.ServiceMode{GlobalJob: &mobyswarm.GlobalJob{}},
				},
				ServiceStatus: &mobyswarm.ServiceStatus{DesiredTasks: 4},
			},
			wantMode:     "global-job",
			wantReplicas: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := NewServiceSummary(tt.service, nil, nil)
			require.Equal(t, tt.wantMode, summary.Mode)
			require.Equal(t, tt.wantReplicas, summary.Replicas)
		})
	}
}
