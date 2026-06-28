package cmdutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveLimit_NilCommandUsesFallback(t *testing.T) {
	require.Equal(t, 25, EffectiveLimit(nil, "containers", "limit", 0, 25))
}
