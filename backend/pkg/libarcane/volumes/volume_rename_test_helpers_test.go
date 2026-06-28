package volumes

import (
	"net/http/httptest"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func newTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}
