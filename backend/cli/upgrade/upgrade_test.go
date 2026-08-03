package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater/labels"
)

func TestNormalizeRecreatedArcaneLabelsInternal(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		want       map[string]string
		wantSource map[string]string
	}{
		{
			name: "legacy server gains current Arcane label",
			labels: map[string]string{
				labels.LabelArcaneLegacyServer: "true",
				labels.LabelUpdater:            "false",
				"com.example.unrelated":        "keep",
			},
			want: map[string]string{
				labels.LabelArcaneLegacyServer: "true",
				labels.LabelArcane:             "true",
				labels.LabelUpdater:            "false",
				"com.example.unrelated":        "keep",
			},
			wantSource: map[string]string{
				labels.LabelArcaneLegacyServer: "true",
				labels.LabelUpdater:            "false",
				"com.example.unrelated":        "keep",
			},
		},
		{
			name: "agent gains current Arcane label",
			labels: map[string]string{
				labels.LabelArcaneAgent: "true",
			},
			want: map[string]string{
				labels.LabelArcane:      "true",
				labels.LabelArcaneAgent: "true",
			},
			wantSource: map[string]string{
				labels.LabelArcaneAgent: "true",
			},
		},
		{
			name: "unrelated labels are preserved without Arcane label",
			labels: map[string]string{
				"com.example.unrelated": "keep",
			},
			want: map[string]string{
				"com.example.unrelated": "keep",
			},
			wantSource: map[string]string{
				"com.example.unrelated": "keep",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRecreatedArcaneLabelsInternal(tt.labels)

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantSource, tt.labels)
		})
	}
}

func TestRefreshRecreatedImageLabelsInternal(t *testing.T) {
	containerLabels := map[string]string{
		"ORG.OPENCONTAINERS.IMAGE.VERSION":  "v2.6.0-next.30",
		"org.opencontainers.image.revision": "old-revision",
		"org.opencontainers.image.authors":  "https://container.example/override",
		"com.docker.compose.image":          "sha256:old-image",
		"com.docker.compose.project":        "arcane",
		labels.LabelArcane:                  "true",
		labels.LabelUpdater:                 "false",
		"com.example.custom":                "keep",
	}
	originalContainerLabels := map[string]string{
		"ORG.OPENCONTAINERS.IMAGE.VERSION":  "v2.6.0-next.30",
		"org.opencontainers.image.revision": "old-revision",
		"org.opencontainers.image.authors":  "https://container.example/override",
		"com.docker.compose.image":          "sha256:old-image",
		"com.docker.compose.project":        "arcane",
		labels.LabelArcane:                  "true",
		labels.LabelUpdater:                 "false",
		"com.example.custom":                "keep",
	}
	targetImageLabels := map[string]string{
		"org.opencontainers.image.version":  "v2.7.0-next.17",
		"org.opencontainers.image.revision": "new-revision",
		"org.opencontainers.image.authors":  "new-author",
		"org.opencontainers.image.title":    "Arcane",
		"com.example.image":                 "must-not-be-copied",
	}
	previousImageLabels := map[string]string{
		"org.opencontainers.image.version":  "v2.6.0-next.30",
		"org.opencontainers.image.revision": "old-revision",
		"org.opencontainers.image.authors":  "old-author",
	}

	got := refreshRecreatedImageLabelsInternal(containerLabels, previousImageLabels, targetImageLabels, "sha256:new-image")

	require.Equal(t, map[string]string{
		"org.opencontainers.image.version":  "v2.7.0-next.17",
		"org.opencontainers.image.revision": "new-revision",
		"org.opencontainers.image.authors":  "https://container.example/override",
		"org.opencontainers.image.title":    "Arcane",
		"com.docker.compose.image":          "sha256:new-image",
		"com.docker.compose.project":        "arcane",
		labels.LabelArcane:                  "true",
		labels.LabelUpdater:                 "false",
		"com.example.custom":                "keep",
	}, got)
	require.Equal(t, originalContainerLabels, containerLabels)
}

func TestRefreshRecreatedImageLabelsInternalKeepsNilWhenNoLabelsExist(t *testing.T) {
	require.Nil(t, refreshRecreatedImageLabelsInternal(nil, nil, nil, ""))
}

func TestRefreshRecreatedContainerLabelsInternalPreservesLabelsWhenTargetInspectFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		assert.Contains(t, r.URL.Path, "/images/target:latest/json",
			"unexpected Docker API request: %s %s", r.Method, r.URL.Path)

		http.Error(w, "target inspect failed", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	containerLabels := map[string]string{
		"org.opencontainers.image.version": "v2.6.0-next.30",
		"org.opencontainers.image.authors": "operator",
		"com.docker.compose.image":         "sha256:old-image",
	}

	got := refreshRecreatedContainerLabelsInternal(
		context.Background(),
		dockerClient,
		containerLabels,
		"sha256:old-image",
		"target:latest",
	)

	require.Equal(t, containerLabels, got)
	got["org.opencontainers.image.version"] = "changed"
	require.Equal(t, "v2.6.0-next.30", containerLabels["org.opencontainers.image.version"])
}

func TestRefreshRecreatedContainerLabelsInternalPreservesOverridesWhenPreviousInspectFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/target:latest/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"sha256:new-image","Config":{"Labels":{"org.opencontainers.image.version":"v2.7.0-next.17","org.opencontainers.image.title":"Arcane"}}}`))
		case strings.Contains(r.URL.Path, "/images/sha256:old-image/json"):
			http.Error(w, "previous inspect failed", http.StatusInternalServerError)
		default:
			assert.Failf(t, "unexpected failure", "unexpected Docker API request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	containerLabels := map[string]string{
		"org.opencontainers.image.version": "v2.6.0-next.30",
		"org.opencontainers.image.authors": "operator",
		"com.docker.compose.image":         "sha256:old-image",
	}

	got := refreshRecreatedContainerLabelsInternal(
		context.Background(),
		dockerClient,
		containerLabels,
		"sha256:old-image",
		"target:latest",
	)

	require.Equal(t, map[string]string{
		"org.opencontainers.image.version": "v2.6.0-next.30",
		"org.opencontainers.image.authors": "operator",
		"org.opencontainers.image.title":   "Arcane",
		"com.docker.compose.image":         "sha256:new-image",
	}, got)
}
