package docker

import "testing"

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
