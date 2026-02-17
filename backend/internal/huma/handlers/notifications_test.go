package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSupportedNotificationTestType(t *testing.T) {
	expected := []string{
		"simple",
		"image-update",
		"batch-image-update",
		"vulnerability-found",
		"prune-report",
		"auto-heal",
	}

	for _, tt := range expected {
		assert.True(t, isSupportedNotificationTestType(tt), "expected %q to be supported", tt)
	}

	assert.False(t, isSupportedNotificationTestType("bogus"))
	assert.False(t, isSupportedNotificationTestType(""))
}

func TestNormalizeNotificationTestType(t *testing.T) {
	assert.Equal(t, "simple", normalizeNotificationTestType(""))
	assert.Equal(t, "simple", normalizeNotificationTestType("  "))
	assert.Equal(t, "auto-heal", normalizeNotificationTestType("auto-heal"))
	assert.Equal(t, "auto-heal", normalizeNotificationTestType("  auto-heal  "))
}
