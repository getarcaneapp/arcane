package projects

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBuildContext_AllowsRemoteGitContext(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://github.com/getarcaneapp/arcane.git#main:docker/app",
		},
	}

	contextDir, err := ResolveBuildContext("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/getarcaneapp/arcane.git#main:docker/app", contextDir)
}

func TestResolveBuildContext_AllowsRemoteGitContextWithoutGitSuffix(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app",
		},
	}

	contextDir, err := ResolveBuildContext("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app", contextDir)
}
