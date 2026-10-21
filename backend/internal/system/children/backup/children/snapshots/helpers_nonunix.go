//go:build !unix

package snapshots

import (
	"os"
)

// Ownership is not represented by uid/gid outside unix.
func preserveOwnership(string, os.FileInfo) error {
	return nil
}
