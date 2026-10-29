package frontend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleIndex = `<!doctype html>
<html lang="en">
	<head>
		<link rel="icon" href="/api/app-images/favicon" />
		<link rel="manifest" href="/app.webmanifest" />
		<link rel="modulepreload" href="/_app/immutable/entry/start.Dl-0sgkl.js">
		<link href="https://fonts.example.com/x.css" rel="stylesheet">
		<link href="//cdn.example.com/y.css" rel="stylesheet">
	</head>
	<body>
		<div style="display: contents">
			<script>
				{
					__sveltekit_12otk0l = {
						base: "",
						version: "1792603566863"
					};
					import("/_app/immutable/entry/start.Dl-0sgkl.js").then(async (kit) => {
						kit.init(__sveltekit_12otk0l);
						const app = await import("/_app/immutable/entry/app.7m--Deg9.js");
						kit.start(app, element);
					});
					if ('serviceWorker' in navigator) {
						const script_url = '/service-worker.js';
					}
				}
			</script>
		</div>
	</body>
</html>`

func TestRewriteIndexForBasePath(t *testing.T) {
	out, err := rewriteIndexForBasePath([]byte(sampleIndex), "/arcane")
	require.NoError(t, err)
	got := string(out)

	for _, want := range []string{
		`href="/arcane/api/app-images/favicon"`,
		`href="/arcane/app.webmanifest"`,
		`href="/arcane/_app/immutable/entry/start.Dl-0sgkl.js"`,
		`base: "/arcane",`,
		`import("/arcane/_app/immutable/entry/start.Dl-0sgkl.js")`,
		`import("/arcane/_app/immutable/entry/app.7m--Deg9.js")`,
		`'/arcane/service-worker.js'`,
		`href="https://fonts.example.com/x.css"`,
		`href="//cdn.example.com/y.css"`,
		`<head><script>(function(b){`,
		`location.replace(b+p+location.search+location.hash)})("/arcane")</script>`,
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, `"/_app/`)
	assert.NotContains(t, got, `/arcane/arcane/`)
}

func TestRewriteIndexForBasePath_UnknownLayoutFails(t *testing.T) {
	_, err := rewriteIndexForBasePath([]byte(`<html><script>import("/_app/x.js")</script></html>`), "/arcane")
	require.Error(t, err)
}
