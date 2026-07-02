package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadMemoryStatValue(t *testing.T) {
	// Trimmed cgroup memory.stat: flat "key value" lines.
	const memoryStat = `anon 1048576
file 2097152
inactive_file 524288
active_file 1572864
total_inactive_file 786432
`
	path := filepath.Join(t.TempDir(), "memory.stat")
	require.NoError(t, os.WriteFile(path, []byte(memoryStat), 0o600))

	// Exact key match: inactive_file must not be shadowed by total_inactive_file.
	value, err := readMemoryStatValue(path, "inactive_file")
	require.NoError(t, err)
	require.Equal(t, int64(524288), value)

	value, err = readMemoryStatValue(path, "total_inactive_file")
	require.NoError(t, err)
	require.Equal(t, int64(786432), value)
}

func TestReadMemoryStatValue_MissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.stat")
	require.NoError(t, os.WriteFile(path, []byte("anon 1048576\n"), 0o600))

	_, err := readMemoryStatValue(path, "inactive_file")
	require.Error(t, err)
}

func TestParseARCStats(t *testing.T) {
	// Abbreviated kstat dump: rows are "name type data".
	const arcstats = `13 1 0x01 98 26656 5560875779 158279494432
name                            type data
hits                            4    1234567
misses                          4    89012
c_min                           4    1073741824
c_max                           4    8589934592
size                            4    7516192768
`
	// size (7 GiB) - c_min (1 GiB) = 6 GiB reclaimable.
	require.Equal(t, uint64(6442450944), parseARCStats(strings.NewReader(arcstats)))
}

func TestParseARCStats_ClampsAndHandlesMissing(t *testing.T) {
	// No arcstats content (non-ZFS host): nothing reclaimable.
	require.Equal(t, uint64(0), parseARCStats(strings.NewReader("")))

	// ARC already at its floor (size <= c_min): clamped to 0, never underflows.
	const atFloor = `size  4  1073741824
c_min 4  1073741824
`
	require.Equal(t, uint64(0), parseARCStats(strings.NewReader(atFloor)))
}
