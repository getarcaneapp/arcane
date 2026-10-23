package updater

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
)

const admissionTestContainers = `[
	{"Id": "aaa111", "Names": ["/web-1"], "Labels": {"com.docker.compose.project": "Shop"}},
	{"Id": "bbb222", "Names": ["/db-1"], "Labels": {"com.docker.compose.project": "shop"}},
	{"Id": "ccc333", "Names": ["/blog-1"], "Labels": {"com.docker.compose.project": "blog"}},
	{"Id": "ddd444", "Names": ["/vpn"]},
	{"Id": "eee555", "Names": ["/torrent"], "HostConfig": {"NetworkMode": "container:vpn"}},
	{"Id": "fff666", "Names": ["/indexer"], "HostConfig": {"NetworkMode": "container:ddd4"}},
	{"Id": "ggg777", "Names": ["/worker"], "Labels": {"com.getarcaneapp.arcane.depends-on": "queue"}},
	{"Id": "hhh888", "Names": ["/queue"]},
	{"Id": "iii999", "Names": ["/solo"]},
	{"Id": "jjj000", "Names": ["/app/cache", "/cache"]},
	{"Id": "kkk111", "Names": ["/app"]}
]`

func admissionTestSummaries(t *testing.T) []container.Summary {
	t.Helper()
	var containers []container.Summary
	require.NoError(t, json.Unmarshal([]byte(admissionTestContainers), &containers))
	return containers
}

func newAdmissionTestService(t *testing.T) (*UpdaterService, *runs.Admission) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(admissionTestContainers))
	}))
	t.Cleanup(server.Close)
	db := setupProjectTestDB(t)
	harness := flowtest.New(t, activity.NewActivityService(db, nil))
	admission := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, admission.Register(harness.Runtime))
	svc, err := NewUpdaterService(
		db,
		nil,
		(&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		admission,
		nil,
	)
	require.NoError(t, err)
	harness.Start(t)
	return svc, admission
}

func TestUpdateAdmissionKeys(t *testing.T) {
	containers := admissionTestSummaries(t)
	keys := func(ids ...string) []types.SchedulerAdmissionKey {
		out := make([]types.SchedulerAdmissionKey, 0, len(ids))
		for _, id := range ids {
			out = append(out, types.SchedulerAdmissionKey{Scope: "updater", ID: id})
		}
		return out
	}
	tests := []struct {
		name    string
		targets []string
		want    []types.SchedulerAdmissionKey
	}{
		{name: "compose project members share the project key", targets: []string{"aaa111"}, want: keys("project:shop")},
		{name: "separate projects stay separate", targets: []string{"ccc333"}, want: keys("project:blog")},
		{
			name:    "network dependent joins its owner",
			targets: []string{"eee555"},
			want:    keys("container:indexer", "container:torrent", "container:vpn", "id:ddd444", "id:eee555", "id:fff666"),
		},
		{
			name:    "network owner covers dependents by name and id prefix",
			targets: []string{"ddd444"},
			want:    keys("container:indexer", "container:torrent", "container:vpn", "id:ddd444", "id:eee555", "id:fff666"),
		},
		{name: "depends-on label is unioned", targets: []string{"hhh888"}, want: keys("container:queue", "container:worker", "id:ggg777", "id:hhh888")},
		{name: "legacy link target joins the linking container", targets: []string{"jjj000"}, want: keys("container:app", "container:cache", "id:jjj000", "id:kkk111")},
		{name: "standalone container has its own key", targets: []string{"iii999"}, want: keys("container:solo", "id:iii999")},
		{name: "missing container falls back to its id and name", targets: []string{"zzz000"}, want: keys("container:zzz000", "id:zzz000")},
		{name: "compose ref locks a project with no containers", targets: []string{"compose:Gone"}, want: keys("project:gone")},
		{name: "multiple targets are sorted and deduplicated", targets: []string{"bbb222", "iii999", "aaa111"}, want: keys("container:solo", "id:iii999", "project:shop")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, updateAdmissionKeys(containers)(tt.targets))
		})
	}
}

func TestUpdaterService_AcquireUpdateUnrelatedKeyAdmitsImmediately(t *testing.T) {
	svc, admission := newAdmissionTestService(t)
	held, admitted, err := admission.TryAcquire(t.Context(), types.SchedulerAdmissionKey{Scope: "updater", ID: "project:shop"})
	require.NoError(t, err)
	require.True(t, admitted)
	defer held.Release(t.Context())

	ctx, release, err := svc.acquireUpdate(t.Context(), []string{"iii999"}, false)
	require.NoError(t, err)
	defer release()

	_, reentrantRelease, err := svc.acquireUpdate(ctx, []string{"iii999"}, false)
	require.NoError(t, err)
	reentrantRelease()
}

func TestUpdaterService_AcquireUpdateWaitsForConflictingKey(t *testing.T) {
	svc, admission := newAdmissionTestService(t)
	held, admitted, err := admission.TryAcquire(t.Context(), types.SchedulerAdmissionKey{Scope: "updater", ID: "container:solo"})
	require.NoError(t, err)
	require.True(t, admitted)

	_, _, err = svc.acquireUpdate(t.Context(), []string{"iii999"}, false)
	require.ErrorIs(t, err, errUpdateBusy)

	acquired := make(chan error, 1)
	go func() {
		_, release, acquireErr := svc.acquireUpdate(t.Context(), []string{"iii999"}, true)
		if acquireErr == nil {
			release()
		}
		acquired <- acquireErr
	}()
	select {
	case acquireErr := <-acquired:
		require.FailNow(t, "acquire did not wait for the conflicting update", "error: %v", acquireErr)
	case <-time.After(400 * time.Millisecond):
	}
	held.Release(t.Context())
	select {
	case acquireErr := <-acquired:
		require.NoError(t, acquireErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "acquire was not admitted after release")
	}
}

func TestUpdaterService_AcquireUpdateFailureReleasesHeldKeys(t *testing.T) {
	svc, admission := newAdmissionTestService(t)
	held, admitted, err := admission.TryAcquire(t.Context(), types.SchedulerAdmissionKey{Scope: "updater", ID: "container:vpn"})
	require.NoError(t, err)
	require.True(t, admitted)
	defer held.Release(t.Context())

	_, _, err = svc.acquireUpdate(t.Context(), []string{"eee555"}, false)
	require.ErrorIs(t, err, errUpdateBusy)

	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	_, _, err = svc.acquireUpdate(ctx, []string{"eee555"}, true)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	for _, id := range []string{"container:indexer", "container:torrent"} {
		lease, free, tryErr := admission.TryAcquire(t.Context(), types.SchedulerAdmissionKey{Scope: "updater", ID: id})
		require.NoError(t, tryErr)
		require.True(t, free, id)
		lease.Release(t.Context())
	}
}
