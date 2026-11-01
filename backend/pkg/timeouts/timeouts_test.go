package timeouts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDefault(t *testing.T) {
	t.Run("adds the default when the context has no deadline", func(t *testing.T) {
		ctx, cancel := WithDefault(t.Context(), time.Minute)
		defer cancel()

		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		assert.WithinDuration(t, time.Now().Add(time.Minute), deadline, 5*time.Second)
	})

	t.Run("keeps an existing deadline", func(t *testing.T) {
		parent, parentCancel := context.WithTimeout(t.Context(), time.Second)
		defer parentCancel()

		ctx, cancel := WithDefault(parent, time.Hour)
		defer cancel()

		parentDeadline, _ := parent.Deadline()
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		assert.Equal(t, parentDeadline, deadline)
	})
}
