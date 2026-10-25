package frontend

import (
	"bytes"
	"regexp"
)

var (
	// Root-relative URLs in attributes and in the dynamic imports of SvelteKit's start script.
	indexRootURLPattern = regexp.MustCompile(`((?:href|src)="|import\(")/([^/"])`)
	// The base SvelteKit reads at runtime (kit.init payload); every other path in the client is
	// derived from it or relative to the chunk that imports it.
	indexKitBasePattern = regexp.MustCompile(`(__sveltekit_[A-Za-z0-9]+\s*=\s*\{\s*base:\s*)""`)
	serviceWorkerURL    = []byte(`'/service-worker.js'`)
)

// rewriteIndexForBasePath adapts the built index.html to the base path Arcane is served
// under: the SvelteKit base and the root-relative asset URLs get the prefix. basePath has no
// trailing slash and is validated by config.Config.BasePath (no quotes or markup).
func rewriteIndexForBasePath(html []byte, basePath string) ([]byte, error) {
	if basePath == "" {
		return html, nil
	}
	if !indexKitBasePattern.Match(html) {
		return nil, ErrIndexNotRecognized
	}
	out := indexKitBasePattern.ReplaceAll(html, []byte(`${1}"`+basePath+`"`))
	out = indexRootURLPattern.ReplaceAll(out, []byte(`${1}`+basePath+`/${2}`))
	out = bytes.ReplaceAll(out, serviceWorkerURL, []byte(`'`+basePath+`/service-worker.js'`))
	return out, nil
}
