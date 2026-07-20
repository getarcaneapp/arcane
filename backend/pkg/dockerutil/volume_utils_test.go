package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestVolumeUsageCacheBehaviorInternal(t *testing.T) {
	t.Run("deduplicates refresh and honors waiter context", func(t *testing.T) {
		resetVolumeUsageCacheForTestInternal()

		started := make(chan struct{})
		release := make(chan struct{})
		var startedOnce sync.Once
		var requests atomic.Int32
		dockerClient := newVolumeUsageTestClientInternal(t, func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			startedOnce.Do(func() { close(started) })
			select {
			case <-release:
				writeVolumeUsageResponseInternal(t, w, "shared", 42)
			case <-r.Context().Done():
			}
		})

		firstResult := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := GetVolumeUsageData(ctx, dockerClient)
			firstResult <- err
		}()

		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("volume usage refresh did not start")
		}

		waiterCtx, waiterCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer waiterCancel()
		waiterStartedAt := time.Now()
		_, err := GetVolumeUsageData(waiterCtx, dockerClient)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(waiterStartedAt), 250*time.Millisecond)

		close(release)
		require.NoError(t, <-firstResult)
		require.Equal(t, int32(1), requests.Load())
	})

	t.Run("serves stale data while one refresh runs", func(t *testing.T) {
		resetVolumeUsageCacheForTestInternal()

		secondStarted := make(chan struct{})
		releaseSecond := make(chan struct{})
		var requests atomic.Int32
		dockerClient := newVolumeUsageTestClientInternal(t, func(w http.ResponseWriter, r *http.Request) {
			requestNumber := requests.Add(1)
			if requestNumber == 1 {
				writeVolumeUsageResponseInternal(t, w, "stale", 10)
				return
			}
			if requestNumber == 2 {
				close(secondStarted)
			}
			select {
			case <-releaseSecond:
				writeVolumeUsageResponseInternal(t, w, "fresh", 20)
			case <-r.Context().Done():
			}
		})

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := GetVolumeUsageData(ctx, dockerClient)
		require.NoError(t, err)

		key := volumeUsageCacheKeyInternal(dockerClient)
		volumeUsageCacheMutex.Lock()
		volumeUsageCacheEntries[key].refreshedAt = time.Now().Add(-volumeUsageCacheTTL)
		volumeUsageCacheMutex.Unlock()

		startedAt := time.Now()
		stale, found := GetVolumeUsageDataStaleWhileRevalidate(ctx, dockerClient)
		require.True(t, found)
		require.Len(t, stale, 1)
		require.Equal(t, "stale", stale[0].Name)
		require.Less(t, time.Since(startedAt), 250*time.Millisecond)

		select {
		case <-secondStarted:
		case <-time.After(time.Second):
			t.Fatal("background volume usage refresh did not start")
		}
		_, _ = GetVolumeUsageDataStaleWhileRevalidate(ctx, dockerClient)
		require.Equal(t, int32(2), requests.Load())

		close(releaseSecond)
		fresh, err := GetVolumeUsageData(ctx, dockerClient)
		require.NoError(t, err)
		require.Len(t, fresh, 1)
		require.Equal(t, "fresh", fresh[0].Name)
	})

	t.Run("isolates and invalidates by daemon host", func(t *testing.T) {
		resetVolumeUsageCacheForTestInternal()

		clientA := newVolumeUsageTestClientInternal(t, func(w http.ResponseWriter, _ *http.Request) {
			writeVolumeUsageResponseInternal(t, w, "host-a", 1)
		})
		clientB := newVolumeUsageTestClientInternal(t, func(w http.ResponseWriter, _ *http.Request) {
			writeVolumeUsageResponseInternal(t, w, "host-b", 2)
		})

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := GetVolumeUsageData(ctx, clientA)
		require.NoError(t, err)
		_, err = GetVolumeUsageData(ctx, clientB)
		require.NoError(t, err)

		cachedA, foundA, _ := volumeUsageCacheSnapshotInternal(volumeUsageCacheKeyInternal(clientA))
		cachedB, foundB, _ := volumeUsageCacheSnapshotInternal(volumeUsageCacheKeyInternal(clientB))
		require.True(t, foundA)
		require.True(t, foundB)
		require.Equal(t, "host-a", cachedA[0].Name)
		require.Equal(t, "host-b", cachedB[0].Name)

		InvalidateVolumeUsageCache(clientA)
		_, foundA, _ = volumeUsageCacheSnapshotInternal(volumeUsageCacheKeyInternal(clientA))
		cachedB, foundB, _ = volumeUsageCacheSnapshotInternal(volumeUsageCacheKeyInternal(clientB))
		require.False(t, foundA)
		require.True(t, foundB)
		require.Equal(t, "host-b", cachedB[0].Name)
	})
}

func newVolumeUsageTestClientInternal(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.55"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dockerClient.Close()) })
	return dockerClient
}

func writeVolumeUsageResponseInternal(t *testing.T, w http.ResponseWriter, name string, size int64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, err := fmt.Fprintf(w, `{"VolumeUsage":{"Items":[{"Name":%q,"UsageData":{"Size":%d,"RefCount":1}}]}}`, name, size)
	require.NoError(t, err)
}

func resetVolumeUsageCacheForTestInternal() {
	volumeUsageCacheMutex.Lock()
	volumeUsageCacheEntries = make(map[string]*volumeUsageCacheEntryInternal)
	volumeUsageCacheGenerations = make(map[string]uint64)
	volumeUsageCacheMutex.Unlock()
}
