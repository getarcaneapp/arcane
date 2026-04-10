package types

import "testing"

func TestConfigLimitForRepos(t *testing.T) {
	cfg := &Config{}
	cfg.SetResourceLimit("repos", 42)

	for _, resource := range []string{"repos", "repo", "git-repositories", "git-repos"} {
		if got := cfg.LimitFor(resource); got != 42 {
			t.Fatalf("LimitFor(%q) = %d, want 42", resource, got)
		}
	}
}

func TestNormalizePaginatedResourceGitOpsSyncAliases(t *testing.T) {
	for _, resource := range []string{
		"gitops-syncs",
		"gitopssyncs",
		"gitops syncs",
		"gitops",
		"gitopssync",
	} {
		if got := NormalizePaginatedResource(resource); got != "gitops-syncs" {
			t.Fatalf("NormalizePaginatedResource(%q) = %q, want %q", resource, got, "gitops-syncs")
		}
	}
}
