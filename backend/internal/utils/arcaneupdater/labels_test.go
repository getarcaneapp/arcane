package arcaneupdater

import (
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArcaneContainer(tt.labels); got != tt.want {
				t.Errorf("IsArcaneContainer() = %v, want %v", got, tt.want)
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
