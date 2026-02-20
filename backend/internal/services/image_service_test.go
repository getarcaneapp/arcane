package services

import (
	"testing"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/getarcaneapp/arcane/types/vulnerability"
	"github.com/stretchr/testify/assert"
)

func TestGetImageIDsFromSummariesInternal(t *testing.T) {
	items := []imagetypes.Summary{
		{ID: "img1"},
		{ID: "img2"},
		{ID: "img1"},
		{ID: ""},
	}

	got := getImageIDsFromSummariesInternal(items)
	assert.Equal(t, []string{"img1", "img2"}, got)
}

func TestApplyVulnerabilitySummariesToItemsInternal(t *testing.T) {
	items := []imagetypes.Summary{
		{ID: "img1"},
		{ID: "img2"},
	}

	summary := &vulnerability.ScanSummary{
		ImageID: "img1",
		Status:  vulnerability.ScanStatusCompleted,
	}
	vulnerabilityMap := map[string]*vulnerability.ScanSummary{
		"img1": summary,
	}

	applyVulnerabilitySummariesToItemsInternal(items, vulnerabilityMap)

	assert.Equal(t, summary, items[0].VulnerabilityScan)
	assert.Nil(t, items[1].VulnerabilityScan)
}
