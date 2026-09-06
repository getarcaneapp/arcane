package event

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestDockerExpectationIdentityAndExpiry(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("abcdef12", 8)
	for _, tc := range []struct {
		name, kind, expectedID, expectedName, actorID, actorName string
		want                                                     bool
	}{
		{"exact ID", "container", id, "", id, "", true},
		{"short ID", "container", id[:12], "", id, "", true},
		{"too short ID", "container", id[:11], "", id, "", false},
		{"name", "container", "", "web", "different", "web", true},
		{"placeholder name", "container", "", "name", "different", "name", false},
		{"image digest", "image", "sha256:" + id[:12], "", "sha256:" + id, "", true},
		{"image reference prefix", "image", "registry/image", "", "registry/image:latest", "", false},
		{"volume hex prefix", "volume", id[:12], "", id, "", false},
		{"exact volume name", "volume", "data", "", "data", "", true},
		{"invalid hex", "container", strings.Repeat("z", 12), "", strings.Repeat("z", 64), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewEventService(nil, nil, nil)
			now := time.Now()
			svc.dockerCorrelation.now = func() time.Time { return now }
			svc.MarkDockerExpectation(tc.kind, tc.expectedID, tc.expectedName)
			require.Equal(t, tc.want, svc.ShouldSuppressDaemonEvent(tc.kind, tc.actorID, tc.actorName, ""))
			require.False(t, svc.ShouldSuppressDaemonEvent("other", tc.actorID, tc.actorName, ""))
			now = now.Add(59 * time.Second)
			require.Equal(t, tc.want, svc.ShouldSuppressDaemonEvent(tc.kind, tc.actorID, tc.actorName, ""))
			now = now.Add(time.Second)
			require.False(t, svc.ShouldSuppressDaemonEvent(tc.kind, tc.actorID, tc.actorName, ""))
		})
	}
}

func TestDockerSuppressionWindowsOverlapAndGrace(t *testing.T) {
	t.Parallel()
	for _, compose := range []bool{false, true} {
		name := "resource"
		if compose {
			name = "compose"
		}
		t.Run(name, func(t *testing.T) {
			svc := NewEventService(nil, nil, nil)
			now := time.Now()
			svc.dockerCorrelation.now = func() time.Time { return now }
			open := func() func() {
				if compose {
					return svc.BeginComposeSuppressionWindow("demo")
				}
				return svc.BeginDockerResourceSuppressionWindow("container", "id", "web")
			}
			first := open()
			second := open()
			first()
			first()
			now = now.Add(time.Hour)
			require.True(t, svc.ShouldSuppressDaemonEvent("container", "id", "web", "demo"))
			if compose {
				require.False(t, svc.ShouldSuppressDaemonEvent("container", "id", "web", "other"))
				require.False(t, svc.ShouldSuppressDaemonEvent("container", "id", "web", ""))
				for _, kind := range []string{"image", "volume", "network"} {
					require.False(t, svc.ShouldSuppressDaemonEvent(kind, "id", "name", ""))
					require.True(t, svc.ShouldSuppressDaemonEvent(kind, "id", "name", "demo"))
					require.False(t, svc.ShouldSuppressDaemonEvent(kind, "id", "name", "other"))
				}
			}
			require.False(t, svc.ShouldSuppressDaemonEvent("container", "unrelated", "other", "other"))
			second()
			second()
			now = now.Add(9 * time.Second)
			for _, kind := range []string{"image", "volume", "network"} {
				require.False(t, svc.ShouldSuppressDaemonEvent(kind, "unrelated", "other", ""))
			}
			require.True(t, svc.ShouldSuppressDaemonEvent("container", "id", "web", "demo"))
			now = now.Add(time.Second)
			require.False(t, svc.ShouldSuppressDaemonEvent("container", "id", "web", "demo"))
			require.False(t, svc.ShouldSuppressDaemonEvent("image", "id", "name", ""))
		})
	}
}

func TestCreateEventCorrelationEligibility(t *testing.T) {
	for _, tc := range []struct {
		name        string
		environment *string
		source      string
		eventType   EventType
		want        bool
	}{
		{"local", new("0"), "", EventTypeContainerStart, true},
		{"implicit local", nil, "", EventTypeContainerStart, true},
		{"remote", new("remote"), "", EventTypeContainerStart, false},
		{"daemon", new("0"), "docker", EventTypeContainerStart, false},
		{"failure", new("0"), "", EventTypeContainerError, false},
		{"container scan", new("0"), "", EventTypeContainerScan, false},
		{"image scan", new("0"), "", EventTypeImageScan, false},
		{"volume backup", new("0"), "", EventTypeVolumeBackupCreate, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			_, err := svc.CreateEvent(t.Context(), CreateEventRequest{Type: tc.eventType, Title: "action", ResourceType: new(strings.SplitN(string(tc.eventType), ".", 2)[0]), ResourceID: new("id"), EnvironmentID: tc.environment, Metadata: database.JSON{"source": tc.source}})
			require.NoError(t, err)
			require.Equal(t, tc.want, svc.ShouldSuppressDaemonEvent(strings.SplitN(string(tc.eventType), ".", 2)[0], "id", "", ""))
		})
	}
}

func TestDockerResourceWindowsCoverLongOperationsInternal(t *testing.T) {
	svc := NewEventService(nil, nil, nil)
	now := time.Now()
	svc.dockerCorrelation.now = func() time.Time { return now }
	closeFirst := svc.BeginDockerResourceSuppressionWindow("image", "", "nginx:latest")
	closeSecond := svc.BeginDockerResourceSuppressionWindow("image", "", "nginx:latest")
	closeFirst()
	closeFirst()
	now = now.Add(time.Hour)
	require.True(t, svc.ShouldSuppressDaemonEvent("image", "nginx:latest", "", ""))
	require.False(t, svc.ShouldSuppressDaemonEvent("image", "redis:latest", "", ""))
	closeSecond()
	now = now.Add(9 * time.Second)
	require.True(t, svc.ShouldSuppressDaemonEvent("image", "nginx:latest", "", ""))
	now = now.Add(time.Second)
	require.False(t, svc.ShouldSuppressDaemonEvent("image", "nginx:latest", "", ""))
}
