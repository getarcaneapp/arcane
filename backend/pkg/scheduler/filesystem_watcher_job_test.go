package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilesystemWatcherJob_ProjectWatcherOptions_UsesConfiguredMaxDepth(t *testing.T) {
	job := &FilesystemWatcherJob{
		projectScanDepth: 1,
	}

	opts := job.projectWatcherOptionsInternal(true)

	assert.Equal(t, 1, opts.MaxDepth)
	assert.True(t, opts.FollowSymlinkDirs)
}
