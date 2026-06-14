package volumehelper

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/stretchr/testify/require"
)

func TestLabels(t *testing.T) {
	labels := Labels()

	require.Equal(t, "true", labels[libarcane.InternalResourceLabel])
	require.Len(t, labels, 1)
}
