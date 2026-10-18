//go:build !unix

package snapshots

import (
	"os"
	"testing"
)

// requireSameOwner is a no-op off Unix, where file ownership (syscall.Stat_t)
// does not apply.
func requireSameOwner(t *testing.T, original, staged os.FileInfo) {
	t.Helper()
}
