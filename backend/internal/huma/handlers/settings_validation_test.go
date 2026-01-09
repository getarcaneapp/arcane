package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectsDirectoryValidation(t *testing.T) {
	tests := []struct {
		name        string
		projectsDir string
		expectValid bool
	}{
		{
			name:        "valid absolute path",
			projectsDir: "/app/data/projects",
			expectValid: true,
		},
		{
			name:        "valid absolute path root",
			projectsDir: "/projects",
			expectValid: true,
		},
		{
			name:        "invalid relative path",
			projectsDir: "data/projects",
			expectValid: false,
		},
		{
			name:        "invalid relative path with dot",
			projectsDir: "./data/projects",
			expectValid: false,
		},
		{
			name:        "invalid relative path parent",
			projectsDir: "../projects",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := strings.HasPrefix(tt.projectsDir, "/")
			assert.Equal(t, tt.expectValid, isValid, "path: %s", tt.projectsDir)
		})
	}
}
