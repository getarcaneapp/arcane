package setupplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeclaredKeys(t *testing.T) {
	content := "# comment\nA=1\nexport B=2\nKEY: v\nMULTI=\"line one\nC=not a key\"\nA=again\n"
	assert.Equal(t, []string{"A", "B", "KEY", "MULTI"}, DeclaredKeys(content))
	assert.Empty(t, DeclaredKeys(""))
}

func TestInjectEnvFileRefs(t *testing.T) {
	content := "TZ=UTC\nDB_PASSWORD=old"
	updated, added, skipped := InjectEnvFileRefs(content, []string{"API_KEY", "DB_PASSWORD", "bad-key", "API_KEY", "ADMIN"})
	assert.Equal(t, []string{"ADMIN", "API_KEY"}, added)
	assert.Equal(t, "TZ=UTC\nDB_PASSWORD=old\n\n"+envRefsHeader+"\nADMIN=${ADMIN}\nAPI_KEY=${API_KEY}\n", updated)
	assert.Equal(t, []SkippedRef{
		{Service: EnvFileTarget, Key: "DB_PASSWORD", Reason: "already set in .env"},
		{Service: EnvFileTarget, Key: "bad-key", Reason: "not a valid variable name"},
	}, skipped)
	assert.Equal(t, []string{"TZ", "DB_PASSWORD", "ADMIN", "API_KEY"}, DeclaredKeys(updated))
	_, refs := ReferencedKeys(updated)["API_KEY"]
	assert.True(t, refs)

	// A file that already ends with a blank line gets no second one.
	updated, _, _ = InjectEnvFileRefs("TZ=UTC\n\n", []string{"X"})
	assert.Equal(t, "TZ=UTC\n\n"+envRefsHeader+"\nX=${X}\n", updated)

	// An empty file starts with the header.
	updated, _, _ = InjectEnvFileRefs("", []string{"X"})
	assert.Equal(t, envRefsHeader+"\nX=${X}\n", updated)

	// Nothing to add leaves the content alone.
	updated, added, _ = InjectEnvFileRefs(content, []string{"TZ"})
	assert.Equal(t, content, updated)
	assert.Empty(t, added)
}

func TestSelfReferencedKeys(t *testing.T) {
	content := "A=${A}\nB=\"${B}\"\nC=$C\nD=${D:-x}\nE=${A}\nF='${F}'\n# G=${G}\n"
	assert.Equal(t, map[string]struct{}{"A": {}, "B": {}, "C": {}}, SelfReferencedKeys(content))
}
