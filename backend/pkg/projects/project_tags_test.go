package projects

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeProjectTags(t *testing.T) {
	tags, err := NormalizeProjectTags([]string{" Database ", "DATABASE", "maintenance-window"})
	require.NoError(t, err)
	require.Equal(t, []string{"database", "maintenance-window"}, tags)

	for _, invalid := range []string{"", "bad,tag", "bad\ntag", strings.Repeat("a", ProjectTagMaxLength+1)} {
		_, err := NormalizeProjectTag(invalid)
		require.Error(t, err, invalid)
	}

	tooMany := make([]string, ProjectTagsPerSourceLimit+1)
	for index := range tooMany {
		tooMany[index] = "tag-" + strings.Repeat("x", index/10) + string(rune('a'+index%10))
	}
	_, err = NormalizeProjectTags(tooMany)
	require.Error(t, err)
}
