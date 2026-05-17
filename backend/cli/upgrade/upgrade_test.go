package upgrade

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
)

func TestMigrationBinaryPathForManagerContainer(t *testing.T) {
	cont := container.InspectResponse{
		Config: &container.Config{
			Labels: map[string]string{
				"com.getarcaneapp.arcane": "true",
			},
		},
	}

	assert.Equal(t, "/app/arcane", migrationBinaryPathForContainerInternal(cont))
}

func TestMigrationBinaryPathForAgentContainer(t *testing.T) {
	tests := []struct {
		name string
		cont container.InspectResponse
	}{
		{
			name: "agent label",
			cont: container.InspectResponse{
				Config: &container.Config{
					Labels: map[string]string{
						"com.getarcaneapp.arcane.agent": "true",
					},
				},
			},
		},
		{
			name: "direct agent env",
			cont: container.InspectResponse{
				Config: &container.Config{
					Env: []string{"AGENT_MODE=true"},
				},
			},
		},
		{
			name: "edge agent env",
			cont: container.InspectResponse{
				Config: &container.Config{
					Env: []string{"EDGE_AGENT=true"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, "/app/arcane-agent", migrationBinaryPathForContainerInternal(tt.cont))
		})
	}
}
