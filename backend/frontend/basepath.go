package frontend

import (
	"bytes"
	"fmt"
	"regexp"
)

var (
	indexRootURLPattern = regexp.MustCompile(`((?:href|src)="|import\(")/([^/"])`)
	indexKitBasePattern = regexp.MustCompile(`(__sveltekit_[A-Za-z0-9]+\s*=\s*\{\s*base:\s*)""`)
	serviceWorkerURL    = []byte(`'/service-worker.js'`)
	headOpenTag         = []byte("<head>")
)

const basePathGuardScript = `<head><script>(function(b){var p=location.pathname;` +
	`if(p!==b&&p.indexOf(b+'/')!==0)location.replace(b+p+location.search+location.hash)})(%q)</script>`

func rewriteIndexForBasePath(html []byte, basePath string) ([]byte, error) {
	if !indexKitBasePattern.Match(html) || !bytes.Contains(html, headOpenTag) {
		return nil, ErrIndexNotRecognized
	}
	out := indexKitBasePattern.ReplaceAll(html, []byte(`${1}"`+basePath+`"`))
	out = bytes.Replace(out, headOpenTag, fmt.Appendf(nil, basePathGuardScript, basePath), 1)
	out = indexRootURLPattern.ReplaceAll(out, []byte(`${1}`+basePath+`/${2}`))
	return bytes.ReplaceAll(out, serviceWorkerURL, []byte(`'`+basePath+`/service-worker.js'`)), nil
}
