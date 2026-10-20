package secretsource

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSecretAddressBlocksMetadataRanges(t *testing.T) {
	for _, address := range []string{
		"169.254.169.254:80", "[fe80::1]:443", "[fe80::1%eth0]:443", "[fd00:ec2::254]:80", "100.100.100.200:80",
		"[::ffff:169.254.169.254]:80", "168.63.129.16:80", "[64:ff9b::a9fe:a9fe]:80",
	} {
		require.ErrorIs(t, checkSecretAddressInternal("tcp", address, nil), errBlockedSecretAddress, address)
	}
	for _, address := range []string{"127.0.0.1:8200", "10.0.0.5:8087", "172.18.0.2:8080", "100.64.1.2:443", "[fd7a:115c:a1e0::1]:443"} {
		assert.NoError(t, checkSecretAddressInternal("tcp", address, nil), address)
	}
}

func TestCheckSecretHostBlocksMetadataNames(t *testing.T) {
	for _, host := range []string{"metadata.google.internal", "METADATA.google.internal.", "169.254.169.254", "fe80::1%eth0"} {
		require.ErrorIs(t, checkSecretHostInternal(host), errBlockedSecretAddress, host)
	}
	for _, host := range []string{"openbao", "vault.example.com", "10.0.0.5", "fd7a:115c:a1e0::1"} {
		assert.NoError(t, checkSecretHostInternal(host), host)
	}
}

func TestSecretHTTPClientReachesPrivateAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()

	client := newSecretHTTPClientInternal(5 * time.Second)
	// Dial directly even when the test environment sets a proxy.
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	transport.Proxy = nil
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	require.NoError(t, err)
	_, err = client.Do(req)
	require.ErrorIs(t, err, errBlockedSecretAddress)
}
