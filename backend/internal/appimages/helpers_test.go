package appimages

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStableVersion(t *testing.T) {
	tests := []struct {
		version string
		stable  bool
	}{
		{"1.2.3", true},
		{"v1.2.3", true},
		{" 1.2.3 ", true},
		{"1.2.3+build.5", true},
		{"1.2.3-next.4", false},
		{"v1.2.3-rc.1", false},
		{"1.2.3-beta", false},
		{"next", false},
		{"dev", false},
		{"snapshot-abc123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			assert.Equal(t, tt.stable, isStableVersion(tt.version))
		})
	}
}
