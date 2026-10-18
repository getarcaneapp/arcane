package imageref

import "testing"

func TestRepoDigestRepository(t *testing.T) {
	tests := []struct {
		name        string
		repoDigests []string
		want        string
		wantOK      bool
	}{
		{name: "nil"},
		{name: "none placeholder only", repoDigests: []string{"<none>@<none>"}},
		{name: "missing at sign", repoDigests: []string{"nginx"}},
		{name: "empty repository", repoDigests: []string{"@sha256:abc"}},
		{
			name:        "first usable entry wins",
			repoDigests: []string{"<none>@<none>", "ghcr.io/getarcaneapp/arcane@sha256:abc", "nginx@sha256:def"},
			want:        "ghcr.io/getarcaneapp/arcane",
			wantOK:      true,
		},
		{
			name:        "registry with port",
			repoDigests: []string{"localhost:5000/app@sha256:abc"},
			want:        "localhost:5000/app",
			wantOK:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := RepoDigestRepository(tt.repoDigests)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("RepoDigestRepository(%q) = (%q, %v), want (%q, %v)", tt.repoDigests, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
