package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/selfupdate"
)

func TestFindChecksumMatchesNextDistPath(t *testing.T) {
	checksums := "abc123  dist/arcane-cli_darwin_arm64_v8.0/arcane-cli\n"

	got, err := findChecksum(checksums, "arcane-cli_darwin_arm64_v8.0/arcane-cli", "arcane-cli_darwin_arm64")

	require.NoError(t, err,
		"findChecksum returned error: %v", err)

	require.Equal(t, "abc123", got,
		"findChecksum = %q, want abc123", got)
}

func TestFindChecksumMatchesArchiveBasename(t *testing.T) {
	checksums := "def456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got, err := findChecksum(checksums, "arcane-cli_linux_amd64.tar.gz")

	require.NoError(t, err,
		"findChecksum returned error: %v", err)

	require.Equal(t, "def456", got,
		"findChecksum = %q, want def456", got)
}

func TestChecksumEntryNames(t *testing.T) {
	checksums := "abc123  ./arcane-cli_darwin_arm64\n\nbad-line\ndef456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got := checksumEntryNames(checksums)
	want := []string{"arcane-cli_darwin_arm64", "dist/arcane-cli_linux_amd64.tar.gz"}
	if len(got) != len(want) {
		require.Len(t, got, len(want),
			"checksumEntryNames length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		require.Equal(t, want[i], got[i],
			"checksumEntryNames[%d] = %q, want %q", i, got[i], want[i])
	}
}

func TestCLIArtifactNamesUseFlatR2BinaryNames(t *testing.T) {
	platformName, err := cliPlatformName()

	require.NoError(t, err,
		"cliPlatformName returned error: %v", err)

	require.False(t, strings.HasPrefix(platformName, "arcane-cli_") || strings.Contains(platformName, "/") || strings.HasSuffix(platformName, ".tar.gz"),
		"platformName = %q, want bare platform name", platformName)
}

func TestFetchCLIChecksumsSignatureVerification(t *testing.T) {
	fixtureDir := "../../internal/selfupdate/testdata"
	checksums, err := os.ReadFile(filepath.Join(fixtureDir, "arcane_2.15.1_checksums.txt"))
	require.NoError(t, err)
	bundleJSON, err := os.ReadFile(filepath.Join(fixtureDir, "arcane_2.15.1_checksums.txt.sigstore.json"))
	require.NoError(t, err)
	trustJSON, err := os.ReadFile(filepath.Join(fixtureDir, "trusted_root.json"))
	require.NoError(t, err)
	trustedRoot, err := root.NewTrustedRootFromJSON(trustJSON)
	require.NoError(t, err)
	key, err := os.ReadFile("../../../cosign.pub")
	require.NoError(t, err)
	verifier, err := selfupdate.NewVerifier(trustedRoot, [][]byte{key})
	require.NoError(t, err)

	for _, tt := range []struct {
		name                   string
		preview                bool
		checksums, bundle      []byte
		status, bundleRequests int
		wantError              string
	}{
		{name: "preview off", checksums: checksums, bundle: []byte("invalid")},
		{name: "verified", preview: true, checksums: checksums, bundle: bundleJSON, bundleRequests: 1},
		{name: "bad bundle", preview: true, checksums: checksums, bundle: []byte("{"), bundleRequests: 1, wantError: "failed to parse checksum signature bundle"},
		{
			name: "altered checksums", preview: true, checksums: append([]byte("x"), checksums[1:]...), bundle: bundleJSON,
			bundleRequests: 1, wantError: "checksum signature verification failed",
		},
		{
			name: "unsigned release", preview: true, checksums: checksums, status: http.StatusNotFound, bundleRequests: 1,
			wantError: "releases before v1.18.0 are unsigned; run `arcane-cli config set previews.newSelfUpdater false` to install them",
		},
		{name: "bundle HTTP failure", preview: true, checksums: checksums, status: http.StatusServiceUnavailable, bundleRequests: 1, wantError: "HTTP 503 Service Unavailable"},
		{
			name: "oversized bundle", preview: true, checksums: checksums, bundle: []byte(strings.Repeat("x", maxCLIBundleSize+1)),
			bundleRequests: 1, wantError: "bundle response exceeds maximum size",
		},
		{name: "oversized checksums", preview: true, checksums: []byte(strings.Repeat("x", maxCLIChecksumSize+1)), wantError: "checksum response exceeds maximum size"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var bundleRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sigstore.json") {
					assert.Equal(t, "application/json", r.Header.Get("Accept"))
					bundleRequests.Add(1)
					if tt.status != 0 {
						w.WriteHeader(tt.status)
					}
					_, _ = w.Write(tt.bundle)
					return
				}
				assert.Equal(t, "text/plain", r.Header.Get("Accept"))
				_, _ = w.Write(tt.checksums)
			}))
			defer server.Close()

			checksumURL := server.URL + "/arcane_2.15.1_checksums.txt"
			got, bundleURL, fetchErr := cliUpdateOptions{}.fetchCLIChecksums(t.Context(), checksumURL, kit.Ternary(tt.preview, verifier, nil))
			require.EqualValues(t, tt.bundleRequests, bundleRequests.Load())
			if tt.wantError != "" {
				require.ErrorContains(t, fetchErr, tt.wantError)
				return
			}
			require.NoError(t, fetchErr)
			require.Equal(t, string(checksums), got)
			require.Equal(t, kit.Ternary(tt.preview, checksumURL+".sigstore.json", ""), bundleURL)
		})
	}
}
