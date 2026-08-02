package projects

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeContentProjectName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"literal name", "name: sabnzbd\nservices:\n  sabnzbd:\n    image: nginx\n", "sabnzbd"},
		{"normalized", "name: My_App 2\nservices: {}\n", "my_app2"},
		{"absent", "services: {}\n", ""},
		{"interpolated", "name: ${PROJECT}\nservices: {}\n", ""},
		{"invalid yaml", "name: [unclosed\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			{
				got := ComposeContentProjectName(tt.content)
				assert.Equal(t, tt.want, got,
					"ComposeContentProjectName() = %q, want %q", got, tt.want)
			}

		})
	}
}

func TestNormalizeProjectName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "with special chars",
			input:    "My Project!",
			expected: "myproject",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeProjectName(tt.input)

			require.Equal(t, tt.expected, got,
				"NormalizeProjectName() = %q, want %q", got, tt.expected)

		})
	}
}
