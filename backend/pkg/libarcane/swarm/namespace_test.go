package swarm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamespaceResolve(t *testing.T) {
	ns := namespace{name: "stack"}

	require.Equal(t, "stack_data", ns.Scope("data"))
	require.Equal(t, "stack_data", ns.Resolve("data", ""))
	require.Equal(t, "custom", ns.Resolve("data", " custom "))
}

func TestValidateStackName(t *testing.T) {
	require.NoError(t, validateStackName("stack-1"))
	require.Error(t, validateStackName("Stack-1"))
	require.Error(t, validateStackName("../stack"))
}
