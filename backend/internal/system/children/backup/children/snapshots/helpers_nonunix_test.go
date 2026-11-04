//go:build !unix

package snapshots

import (
	"os"
	"testing"
)

func requireSameOwner(*testing.T, os.FileInfo, os.FileInfo) {}
