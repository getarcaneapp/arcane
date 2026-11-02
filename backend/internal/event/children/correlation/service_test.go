package correlation

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"kit.getarcane.app"
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
			now := time.Now()
			svc := NewService(func() time.Time { return now })
			svc.MarkExpectation(tc.kind, tc.expectedID, tc.expectedName)
			require.Equal(t, tc.want, svc.ShouldSuppress(tc.kind, tc.actorID, tc.actorName, ""))
			require.False(t, svc.ShouldSuppress("other", tc.actorID, tc.actorName, ""))
			now = now.Add(59 * time.Second)
			require.Equal(t, tc.want, svc.ShouldSuppress(tc.kind, tc.actorID, tc.actorName, ""))
			now = now.Add(time.Second)
			require.False(t, svc.ShouldSuppress(tc.kind, tc.actorID, tc.actorName, ""))
		})
	}
}

func TestDockerSuppressionWindowsOverlapAndGrace(t *testing.T) {
	t.Parallel()
	for _, compose := range []bool{false, true} {
		name := kit.Ternary(compose, "compose", "resource")
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			svc := NewService(func() time.Time { return now })
			open := func() func() {
				if compose {
					return svc.BeginComposeWindow("demo")
				}
				return svc.BeginResourceWindow("container", "id", "web")
			}
			first := open()
			second := open()
			first()
			first()
			now = now.Add(time.Hour)
			require.True(t, svc.ShouldSuppress("container", "id", "web", "demo"))
			if compose {
				require.False(t, svc.ShouldSuppress("container", "id", "web", "other"))
				require.False(t, svc.ShouldSuppress("container", "id", "web", ""))
				for _, kind := range []string{"image", "volume", "network"} {
					require.False(t, svc.ShouldSuppress(kind, "id", "name", ""))
					require.True(t, svc.ShouldSuppress(kind, "id", "name", "demo"))
					require.False(t, svc.ShouldSuppress(kind, "id", "name", "other"))
				}
			}
			require.False(t, svc.ShouldSuppress("container", "unrelated", "other", "other"))
			second()
			second()
			now = now.Add(9 * time.Second)
			for _, kind := range []string{"image", "volume", "network"} {
				require.False(t, svc.ShouldSuppress(kind, "unrelated", "other", ""))
			}
			require.True(t, svc.ShouldSuppress("container", "id", "web", "demo"))
			now = now.Add(time.Second)
			require.False(t, svc.ShouldSuppress("container", "id", "web", "demo"))
			require.False(t, svc.ShouldSuppress("image", "id", "name", ""))
		})
	}
}

func TestDockerResourceWindowsCoverLongOperations(t *testing.T) {
	now := time.Now()
	svc := NewService(func() time.Time { return now })
	closeFirst := svc.BeginResourceWindow("image", "", "nginx:latest")
	closeSecond := svc.BeginResourceWindow("image", "", "nginx:latest")
	closeFirst()
	closeFirst()
	now = now.Add(time.Hour)
	require.True(t, svc.ShouldSuppress("image", "nginx:latest", "", ""))
	require.False(t, svc.ShouldSuppress("image", "redis:latest", "", ""))
	closeSecond()
	now = now.Add(9 * time.Second)
	require.True(t, svc.ShouldSuppress("image", "nginx:latest", "", ""))
	now = now.Add(time.Second)
	require.False(t, svc.ShouldSuppress("image", "nginx:latest", "", ""))
}
