package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestPaginateContainerProjectGroupsKeepsProjectWhole(t *testing.T) {
	items := []containertypes.Summary{
		newGroupedContainerSummary("other-1", "other-1"),
		newGroupedContainerSummary("other-2", "other-2"),
		newGroupedContainerSummary("other-3", "other-3"),
		newGroupedContainerSummary("other-4", "other-4"),
		newGroupedContainerSummary("other-5", "other-5"),
		newGroupedContainerSummary("other-6", "other-6"),
		newGroupedContainerSummary("other-7", "other-7"),
		newGroupedContainerSummary("other-8", "other-8"),
		newGroupedContainerSummary("other-9", "other-9"),
		newGroupedContainerSummary("other-10", "other-10"),
		newGroupedContainerSummary("other-11", "other-11"),
		newGroupedContainerSummary("other-12", "other-12"),
		newGroupedContainerSummary("other-13", "other-13"),
		newGroupedContainerSummary("other-14", "other-14"),
		newGroupedContainerSummary("other-15", "other-15"),
		newGroupedContainerSummary("other-16", "other-16"),
		newGroupedContainerSummary("other-17", "other-17"),
		newGroupedContainerSummary("other-18", "other-18"),
		newGroupedContainerSummary("immich-server", "immich"),
		newGroupedContainerSummary("immich-ml", "immich"),
		newGroupedContainerSummary("immich-redis", "immich"),
		newGroupedContainerSummary("immich-postgres", "immich"),
	}

	groupedItems, resp := paginateContainerProjectGroupsInternal(
		pagination.FilterResult[containertypes.Summary]{Items: items, TotalCount: int64(len(items)), TotalAvailable: int64(len(items))},
		pagination.QueryParams{PaginationParams: pagination.PaginationParams{Start: 0, Limit: 20}},
	)

	require.Len(t, groupedItems, 19)
	require.Equal(t, int64(1), resp.TotalPages)
	require.Equal(t, 1, resp.CurrentPage)
	require.Equal(t, 20, resp.ItemsPerPage)
	require.Equal(t, int64(22), resp.TotalItems)

	projectCounts := make(map[string]int)
	for _, group := range groupedItems {
		projectCounts[group.GroupName] += len(group.Items)
	}

	require.Equal(t, 4, projectCounts["immich"])
	require.Equal(t, 1, projectCounts["other-1"])
	require.Equal(t, 1, projectCounts["other-18"])
}

func TestGroupContainersByProjectUsesNoProjectBucket(t *testing.T) {
	groups := groupContainersByProjectInternal([]containertypes.Summary{
		{ID: "1", Labels: map[string]string{"com.docker.compose.project": "alpha"}},
		{ID: "2", Labels: map[string]string{}},
		{ID: "3", Labels: nil},
	})

	require.Len(t, groups, 2)
	require.Equal(t, "alpha", groups[0].GroupName)
	require.Len(t, groups[0].Items, 1)
	require.Equal(t, containerNoProjectGroup, groups[1].GroupName)
	require.Len(t, groups[1].Items, 2)
	require.Equal(t, containerNoProjectGroup, getContainerProjectNameInternal(groups[1].Items[0]))
	require.Equal(t, containerNoProjectGroup, getContainerProjectNameInternal(groups[1].Items[1]))
}

func TestBuildCleanNetworkingConfigInternalPreservesEndpointSettings(t *testing.T) {
	containerInspect := container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					Aliases:    []string{"svc"},
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
					IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: netip.MustParseAddr("172.17.0.5")},
				},
			},
		},
	}

	out := buildCleanNetworkingConfigInternal(containerInspect, "1.44")
	require.NotNil(t, out)
	require.Contains(t, out.EndpointsConfig, "bridge")
	require.Equal(t, []string{"svc"}, out.EndpointsConfig["bridge"].Aliases)
	require.Equal(t, netip.MustParseAddr("172.17.0.2"), out.EndpointsConfig["bridge"].IPAddress)
	require.Nil(t, out.EndpointsConfig["bridge"].IPAMConfig)
}

func TestStreamContainerLogs_NonTTYFollowDemultiplexesStdoutAndStderr(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "stdout line\n")
	writeDockerLogFrameInternal(t, &stream, 2, "stderr line\n")

	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.streamContainerLogsInternal(t.Context(), io.NopCloser(bytes.NewReader(stream.Bytes())), logsChan, true, false)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"stdout line", "[STDERR] stderr line"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogs_TTYFollowStreamsRawOutput(t *testing.T) {
	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.streamContainerLogsInternal(t.Context(), io.NopCloser(strings.NewReader("first line\nsecond line")), logsChan, true, true)
	require.NoError(t, err)

	require.Equal(t, []string{"first line", "second line"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogs_NonTTYSnapshotDemultiplexesStdoutAndStderr(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "stdout snapshot\n")
	writeDockerLogFrameInternal(t, &stream, 2, "stderr snapshot\n")

	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.streamContainerLogsInternal(t.Context(), io.NopCloser(bytes.NewReader(stream.Bytes())), logsChan, false, false)
	require.NoError(t, err)

	require.Equal(t, []string{"stdout snapshot", "[STDERR] stderr snapshot"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogs_TTYSnapshotStreamsRawOutput(t *testing.T) {
	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.streamContainerLogsInternal(t.Context(), io.NopCloser(strings.NewReader("snapshot line\ntrailing line")), logsChan, false, true)
	require.NoError(t, err)

	require.Equal(t, []string{"snapshot line", "trailing line"}, drainLogLinesInternal(logsChan))
}

func TestReadLogsFromReader_HandlesLongLinesAndPartialEOF(t *testing.T) {
	longLine := strings.Repeat("a", 70*1024)
	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.readLogsFromReader(t.Context(), strings.NewReader(longLine+"\npartial tail"), logsChan, "")
	require.NoError(t, err)

	require.Equal(t, []string{longLine, "partial tail"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogs_TTYPythonLikeFollowDoesNotReturnEmptyLogs(t *testing.T) {
	service := &ContainerService{}
	logsChan := make(chan string, 4)

	err := service.streamContainerLogsInternal(
		t.Context(),
		io.NopCloser(strings.NewReader("2026-03-22 10:15:00 - INFO - Starting miner\n2026-03-22 10:15:01 - INFO - Connected")),
		logsChan,
		true,
		true,
	)
	require.NoError(t, err)

	lines := drainLogLinesInternal(logsChan)
	require.NotEmpty(t, lines)
	require.Equal(t, []string{
		"2026-03-22 10:15:00 - INFO - Starting miner",
		"2026-03-22 10:15:01 - INFO - Connected",
	}, lines)
}

func TestStreamMultiplexedLogs_ContextCancelDoesNotDeadlock(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "line 1\n")
	writeDockerLogFrameInternal(t, &stream, 1, "line 2\n")
	writeDockerLogFrameInternal(t, &stream, 1, "line 3\n")

	logsChan := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- streamMultiplexedLogs(ctx, io.NopCloser(bytes.NewReader(stream.Bytes())), logsChan)
	}()

	require.Eventually(t, func() bool {
		return len(logsChan) == 1
	}, time.Second, 10*time.Millisecond)

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("streamMultiplexedLogs did not exit after cancellation")
	}
}

func TestReadAllLogs_ContextCancelClosesReader(t *testing.T) {
	service := &ContainerService{}
	logsChan := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &blockingReadCloser{readStarted: make(chan struct{}), closeCalled: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- service.readAllLogs(ctx, reader, logsChan)
	}()

	select {
	case <-reader.readStarted:
	case <-time.After(time.Second):
		t.Fatal("readAllLogs did not start reading")
	}

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("readAllLogs did not exit after cancellation")
	}

	select {
	case <-reader.closeCalled:
	case <-time.After(time.Second):
		t.Fatal("readAllLogs did not close the reader on cancellation")
	}
}

func newGroupedContainerSummary(name string, project string) containertypes.Summary {
	labels := map[string]string{}
	if project != "" {
		labels["com.docker.compose.project"] = project
	}

	return containertypes.Summary{
		ID:     name,
		Names:  []string{name},
		Labels: labels,
		State:  "running",
	}
}

func drainLogLinesInternal(logsChan chan string) []string {
	close(logsChan)

	lines := make([]string, 0, len(logsChan))
	for line := range logsChan {
		lines = append(lines, line)
	}

	return lines
}

func writeDockerLogFrameInternal(t *testing.T, buffer *bytes.Buffer, streamType byte, payload string) {
	t.Helper()

	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	_, err := buffer.Write(header)
	require.NoError(t, err)
	_, err = buffer.WriteString(payload)
	require.NoError(t, err)
}

type blockingReadCloser struct {
	readStarted chan struct{}
	closeCalled chan struct{}
}

func (r *blockingReadCloser) Read(_ []byte) (int, error) {
	select {
	case <-r.readStarted:
	default:
		close(r.readStarted)
	}

	<-r.closeCalled
	return 0, io.EOF
}

func (r *blockingReadCloser) Close() error {
	select {
	case <-r.closeCalled:
	default:
		close(r.closeCalled)
	}
	return nil
}
