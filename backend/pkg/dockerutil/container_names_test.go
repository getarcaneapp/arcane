package docker

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestContainerNameFromNames(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{
			name:  "single name with slash",
			names: []string{"/myapp"},
			want:  "myapp",
		},
		{
			name:  "single name without slash",
			names: []string{"myapp"},
			want:  "myapp",
		},
		{
			name:  "multiple names uses first",
			names: []string{"/myapp", "/myapp-alias"},
			want:  "myapp",
		},
		{
			name:  "no names",
			names: []string{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainerNameFromNames(tt.names); got != tt.want {
				t.Errorf("ContainerNameFromNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerSummaryName(t *testing.T) {
	tests := []struct {
		name string
		cnt  container.Summary
		want string
	}{
		{
			name: "uses Docker name",
			cnt:  container.Summary{Names: []string{"/myapp"}, ID: "abc123456789"},
			want: "myapp",
		},
		{
			name: "falls back to short ID",
			cnt:  container.Summary{Names: []string{}, ID: "abc123456789"},
			want: "abc123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainerSummaryName(tt.cnt); got != tt.want {
				t.Errorf("ContainerSummaryName() = %v, want %v", got, tt.want)
			}
		})
	}
}
