package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
)

func newLogsTestServiceInternal(t *testing.T, tty bool, logsBody []byte, gotQuery *url.Values) *ContainerService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch dockerTestPathInternal(r.URL.Path) {
		case "/containers/container-1/json":
			ttyJSON := "false"
			if tty {
				ttyJSON = "true"
			}
			_, _ = io.WriteString(w, `{"Id":"0123456789abcdef0123456789abcdef","Config":{"Tty":`+ttyJSON+`}}`)
		case "/containers/container-1/logs":
			*gotQuery = r.URL.Query()
			_, _ = w.Write(logsBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return NewContainerService(
		event.NewEventService(setupProjectTestDBInternal(t), nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)
}

func TestContainerServiceDownloadLogsDemultiplexesNonTTYInternal(t *testing.T) {
	var frames bytes.Buffer
	longLine := strings.Repeat("x", 70*1024) + "\n"
	for _, frame := range []struct {
		stream  byte
		payload string
	}{
		{1, "out one\n"},
		{2, "err one\n"},
		{1, "\n"},
		{1, longLine},
		{2, "err two\n"},
	} {
		header := make([]byte, 8)
		header[0] = frame.stream
		binary.BigEndian.PutUint32(header[4:], uint32(len(frame.payload)))
		frames.Write(header)
		frames.WriteString(frame.payload)
	}

	var gotQuery url.Values
	svc := newLogsTestServiceInternal(t, false, frames.Bytes(), &gotQuery)

	reader, filename, err := svc.DownloadLogs(context.Background(), "container-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	require.Equal(t, "container-0123456789ab-logs.log", filename)
	require.Equal(t, "out one\nerr one\n\n"+longLine+"err two\n", string(content))
	require.Equal(t, "1", gotQuery.Get("stdout"))
	require.Equal(t, "1", gotQuery.Get("stderr"))
	require.Contains(t, []string{"", "all"}, gotQuery.Get("tail"))
	require.Equal(t, "1", gotQuery.Get("timestamps"))
	require.NotEqual(t, "1", gotQuery.Get("follow"))
	require.Empty(t, gotQuery.Get("since"))
}

func TestContainerServiceDownloadLogsPassesTTYOutputThroughInternal(t *testing.T) {
	var gotQuery url.Values
	svc := newLogsTestServiceInternal(t, true, []byte("raw tty line\r\nno trailing newline"), &gotQuery)

	reader, filename, err := svc.DownloadLogs(context.Background(), "container-1")
	require.NoError(t, err)

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	require.Equal(t, "container-0123456789ab-logs.log", filename)
	require.Equal(t, "raw tty line\r\nno trailing newline", string(content))
}
