package handlers

import (
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/utils/pathmapper"
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
		{
			name:        "valid mapping format",
			projectsDir: "/app/data/projects:D:/host/path",
			expectValid: true,
		},
		{
			name:        "valid Windows drive path",
			projectsDir: "C:/projects",
			expectValid: true,
		},
		{
			name:        "invalid single colon not Windows",
			projectsDir: "invalid:path",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the validation logic from UpdateSettings handler
			path := tt.projectsDir
			isValid := true
			
			// Check if it's a Windows drive path first
			if pathmapper.IsWindowsDrivePath(path) {
				// Valid Windows path
			} else if strings.Contains(path, ":") {
				// Check if it's a mapping format (container:host)
				parts := strings.SplitN(path, ":", 2)
				if len(parts) == 2 {
					// Validate container path (first part)
					if !strings.HasPrefix(parts[0], "/") && !pathmapper.IsWindowsDrivePath(parts[0]) {
						isValid = false
					}
				} else {
					// Single colon but not Windows drive path or valid mapping
					isValid = false
				}
			} else if !strings.HasPrefix(path, "/") {
				// No colon and doesn't start with / - must be relative
				isValid = false
			}
			
			assert.Equal(t, tt.expectValid, isValid, "path: %s", tt.projectsDir)
		})
	}
}
