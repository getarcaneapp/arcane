package imageref

import (
	"slices"
	"strings"

	"kit.getarcane.app/updater/digest"
	"kit.getarcane.app/updater/refs"
)

// SplitRepositoryReferences moves digest names returned in RepoTags by the
// containerd image store into RepoDigests without changing the Docker image.
func SplitRepositoryReferences(repoTags, repoDigests []string) ([]string, []string) {
	tags := make([]string, 0, len(repoTags))
	digests := slices.Clone(repoDigests)
	for _, repoTag := range repoTags {
		if refs.IsDigestPinnedReference(repoTag) {
			if !slices.Contains(digests, repoTag) {
				digests = append(digests, repoTag)
			}
			continue
		}
		tags = append(tags, repoTag)
	}
	return tags, digests
}

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

// RepositoryDigests returns the digests in an image's RepoDigests that belong
// to imageRef's repository.
func RepositoryDigests(repoDigests []string, imageRef string) []string {
	requested, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return nil
	}
	var digests []string
	for _, repoDigest := range repoDigests {
		digestValue, ok := digest.FromReferenceSuffix(repoDigest)
		repository, parseErr := refs.NormalizeReference(repoDigest)
		if ok && parseErr == nil && repository.RegistryHost == requested.RegistryHost && repository.Repository == requested.Repository {
			digests = append(digests, digestValue)
		}
	}
	return digests
}
