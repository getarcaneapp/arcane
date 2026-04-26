package updater

import (
	"errors"
	"testing"
)

func TestIsArcaneContainer(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   false,
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   false,
		},
		{
			name:   "arcane label true",
			labels: map[string]string{LabelArcane: "true"},
			want:   true,
		},
		{
			name:   "arcane label 1",
			labels: map[string]string{LabelArcane: "1"},
			want:   true,
		},
		{
			name:   "arcane label yes",
			labels: map[string]string{LabelArcane: "yes"},
			want:   true,
		},
		{
			name:   "arcane label on",
			labels: map[string]string{LabelArcane: "on"},
			want:   true,
		},
		{
			name:   "arcane label false",
			labels: map[string]string{LabelArcane: "false"},
			want:   false,
		},
		{
			name:   "arcane label TRUE uppercase",
			labels: map[string]string{LabelArcane: "TRUE"},
			want:   true,
		},
		{
			name:   "arcane label with whitespace",
			labels: map[string]string{LabelArcane: "  true  "},
			want:   true,
		},
		{
			name:   "case insensitive label key",
			labels: map[string]string{"COM.GETARCANEAPP.ARCANE": "true"},
			want:   true,
		},
		{
			name:   "agent label true",
			labels: map[string]string{LabelArcaneAgent: "true"},
			want:   true,
		},
		{
			name:   "agent label with whitespace",
			labels: map[string]string{LabelArcaneAgent: "  yes  "},
			want:   true,
		},
		{
			name:   "agent label false",
			labels: map[string]string{LabelArcaneAgent: "false"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArcaneContainer(tt.labels); got != tt.want {
				t.Errorf("IsArcaneContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsArcaneAgentContainer(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   false,
		},
		{
			name:   "agent label true",
			labels: map[string]string{LabelArcaneAgent: "true"},
			want:   true,
		},
		{
			name:   "agent label case insensitive key",
			labels: map[string]string{"COM.GETARCANEAPP.ARCANE.AGENT": "on"},
			want:   true,
		},
		{
			name:   "core arcane label only",
			labels: map[string]string{LabelArcane: "true"},
			want:   false,
		},
		{
			name:   "agent label false",
			labels: map[string]string{LabelArcaneAgent: "off"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArcaneAgentContainer(tt.labels); got != tt.want {
				t.Errorf("IsArcaneAgentContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsArcaneServerContainer(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   false,
		},
		{
			name:   "server label true",
			labels: map[string]string{LabelArcane: "true"},
			want:   true,
		},
		{
			name:   "server label case insensitive key",
			labels: map[string]string{"COM.GETARCANEAPP.ARCANE": "on"},
			want:   true,
		},
		{
			name:   "server label false",
			labels: map[string]string{LabelArcane: "off"},
			want:   false,
		},
		{
			name:   "agent label only",
			labels: map[string]string{LabelArcaneAgent: "true"},
			want:   false,
		},
		{
			name: "agent label wins over server label",
			labels: map[string]string{
				LabelArcane:      "true",
				LabelArcaneAgent: "true",
			},
			want: false,
		},
		{
			name: "false agent label does not exclude server",
			labels: map[string]string{
				LabelArcane:      "true",
				LabelArcaneAgent: "false",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArcaneServerContainer(tt.labels); got != tt.want {
				t.Errorf("IsArcaneServerContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUpdateDisabled(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "nil labels - default enabled",
			labels: nil,
			want:   false,
		},
		{
			name:   "empty labels - default enabled",
			labels: map[string]string{},
			want:   false,
		},
		{
			name:   "no updater label - default enabled",
			labels: map[string]string{"other": "value"},
			want:   false,
		},
		{
			name:   "updater label false",
			labels: map[string]string{LabelUpdater: "false"},
			want:   true,
		},
		{
			name:   "updater label 0",
			labels: map[string]string{LabelUpdater: "0"},
			want:   true,
		},
		{
			name:   "updater label no",
			labels: map[string]string{LabelUpdater: "no"},
			want:   true,
		},
		{
			name:   "updater label off",
			labels: map[string]string{LabelUpdater: "off"},
			want:   true,
		},
		{
			name:   "updater label true - enabled",
			labels: map[string]string{LabelUpdater: "true"},
			want:   false,
		},
		{
			name:   "updater label FALSE uppercase",
			labels: map[string]string{LabelUpdater: "FALSE"},
			want:   true,
		},
		{
			name:   "updater label with whitespace",
			labels: map[string]string{LabelUpdater: "  false  "},
			want:   true,
		},
		{
			name:   "case insensitive label key",
			labels: map[string]string{"COM.GETARCANEAPP.ARCANE.UPDATER": "false"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUpdateDisabled(tt.labels); got != tt.want {
				t.Errorf("IsUpdateDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldDisableArcaneServerRedeploy(t *testing.T) {
	tests := []struct {
		name               string
		labels             map[string]string
		containerID        string
		currentContainerID string
		currentErr         error
		want               bool
	}{
		{
			name:               "current Arcane server container",
			labels:             map[string]string{LabelArcane: "true"},
			containerID:        "abcdef1234567890",
			currentContainerID: "abcdef1234567890",
			want:               true,
		},
		{
			name:               "current Arcane server container with short detected ID",
			labels:             map[string]string{LabelArcane: "true"},
			containerID:        "abcdef1234567890",
			currentContainerID: "abcdef123456",
			want:               true,
		},
		{
			name:               "different Arcane server container",
			labels:             map[string]string{LabelArcane: "true"},
			containerID:        "abcdef1234567890",
			currentContainerID: "123456abcdef7890",
			want:               false,
		},
		{
			name:        "fail closed when current container cannot be detected",
			labels:      map[string]string{LabelArcane: "true"},
			containerID: "abcdef1234567890",
			currentErr:  errors.New("not in docker"),
			want:        true,
		},
		{
			name:               "agent container remains redeployable",
			labels:             map[string]string{LabelArcaneAgent: "true"},
			containerID:        "abcdef1234567890",
			currentContainerID: "abcdef1234567890",
			want:               false,
		},
		{
			name: "agent label excludes Arcane server label",
			labels: map[string]string{
				LabelArcane:      "true",
				LabelArcaneAgent: "true",
			},
			containerID:        "abcdef1234567890",
			currentContainerID: "abcdef1234567890",
			want:               false,
		},
		{
			name:               "non-Arcane container",
			labels:             map[string]string{"app": "demo"},
			containerID:        "abcdef1234567890",
			currentContainerID: "abcdef1234567890",
			want:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldDisableArcaneServerRedeploy(tt.labels, tt.containerID, tt.currentContainerID, tt.currentErr)
			if got != tt.want {
				t.Errorf("ShouldDisableArcaneServerRedeploy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStopSignal(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   "",
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   "",
		},
		{
			name:   "no stop signal label",
			labels: map[string]string{"other": "value"},
			want:   "",
		},
		{
			name:   "SIGTERM",
			labels: map[string]string{LabelStopSignal: "SIGTERM"},
			want:   "SIGTERM",
		},
		{
			name:   "SIGINT",
			labels: map[string]string{LabelStopSignal: "SIGINT"},
			want:   "SIGINT",
		},
		{
			name:   "SIGKILL",
			labels: map[string]string{LabelStopSignal: "SIGKILL"},
			want:   "SIGKILL",
		},
		{
			name:   "lowercase signal",
			labels: map[string]string{LabelStopSignal: "sigterm"},
			want:   "SIGTERM",
		},
		{
			name:   "signal with whitespace",
			labels: map[string]string{LabelStopSignal: "  SIGTERM  "},
			want:   "SIGTERM",
		},
		{
			name:   "case insensitive label key",
			labels: map[string]string{"COM.GETARCANEAPP.ARCANE.STOP-SIGNAL": "SIGINT"},
			want:   "SIGINT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetStopSignal(tt.labels); got != tt.want {
				t.Errorf("GetStopSignal() = %v, want %v", got, tt.want)
			}
		})
	}
}
