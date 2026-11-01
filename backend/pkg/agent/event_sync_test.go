package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishEventToManager_NoActiveTunnel(t *testing.T) {
	activeAgentEvents.Store(nil)

	err := PublishEventToManager(t.Context(), &TunnelEvent{
		Type:  "container.start",
		Title: "Container started",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoActiveAgentTunnel)
}
