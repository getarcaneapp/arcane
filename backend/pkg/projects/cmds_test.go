package projects

import (
	"context"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/require"
)

func TestComposeStopSkipsWhenNoServicesSpecified(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:9")

	err := ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, nil)
	require.NoError(t, err)

	err = ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, []string{})
	require.NoError(t, err)
}
