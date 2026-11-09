package projects

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProtectedProjectFilePaths_IncludesComposeOverrideCandidates(t *testing.T) {
	t.Parallel()

	protected := ProtectedProjectFilePaths("compose.yaml")

	for _, candidate := range ComposeOverrideFileCandidates {
		assert.Truef(t, protected[candidate], "expected %q to be protected", candidate)
	}
}
