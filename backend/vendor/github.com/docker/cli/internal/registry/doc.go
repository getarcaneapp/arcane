// Package registry is a fork of [github.com/docker/docker/registry], taken
// at commit [moby@49306c6]. Git history  was not preserved in this fork,
// but can be found using the URLs provided.
//
// This fork was created to remove the dependency on the "Moby" codebase,
// and because the CLI only needs a subset of its features. The original
// package was written specifically for use in the daemon code, and includes
// functionality that cannot be used in the CLI.
//
// [github.com/docker/docker/registry]: https://pkg.go.dev/github.com/docker/docker@v28.3.2+incompatible/registry
// [moby@49306c6]: https://github.com/moby/moby/tree/49306c607b72c5bf0a8e426f5a9760fa5ef96ea0/registry
package registry
