package projects

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

// The listing feeds the template preview, so every variable must carry the name
// and value Compose reads from its line rather than the raw text of that line.
func TestParseEnvContentListsComposeEnvFileValues(t *testing.T) {
	t.Parallel()

	content := "export EXPORTED=exported\n" +
		"COMMENTED=value # trailing comment\n" +
		"QUOTED=\"kept # inside quotes\"\n" +
		"ANNOTATED='quoted' # trailing comment\n" +
		"UNSPACED=value#kept\n" +
		"DOTTED.key.with-dash=dotted\n" +
		"EMPTY=\n"

	require.Equal(t, []types.EnvVariable{
		{Key: "EXPORTED", Value: "exported"},
		{Key: "COMMENTED", Value: "value"},
		{Key: "QUOTED", Value: "kept # inside quotes"},
		{Key: "ANNOTATED", Value: "quoted"},
		{Key: "UNSPACED", Value: "value#kept"},
		{Key: "DOTTED.key.with-dash", Value: "dotted"},
		{Key: "EMPTY", Value: ""},
	}, ParseEnvContent(content))
}
