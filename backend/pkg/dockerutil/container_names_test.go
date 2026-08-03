package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
			{
				got := ContainerNameFromNames(tt.names)
				assert.Equal(t, tt.want, got,
					"ContainerNameFromNames() = %v, want %v", got, tt.want)
			}

		})
	}
}
