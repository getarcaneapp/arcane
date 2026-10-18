package imageref

import "strings"

// RepoDigestRepository returns the repository of the first usable
// "repo@digest" entry in an image's RepoDigests.
func RepoDigestRepository(repoDigests []string) (string, bool) {
	for _, repoDigest := range repoDigests {
		if repository, _, found := strings.CutLast(repoDigest, "@"); repoDigest != "<none>@<none>" && found && repository != "" {
			return repository, true
		}
	}
	return "", false
}
