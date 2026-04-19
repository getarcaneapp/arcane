package libbuild

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitBuildContextSource(t *testing.T) {
	t.Run("plain repository url", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("https://github.com/getarcaneapp/arcane.git")
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, source)
		assert.Equal(t, "https://github.com/getarcaneapp/arcane.git", source.RepositoryURL)
		assert.Empty(t, source.Ref)
		assert.Empty(t, source.Subdir)
	})

	t.Run("ref and subdir", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("https://github.com/getarcaneapp/arcane.git#main:docker/app")
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, source)
		assert.Equal(t, "main", source.Ref)
		assert.Equal(t, "docker/app", source.Subdir)
	})

	t.Run("ssh url", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("git@github.com:getarcaneapp/arcane.git#dev")
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, source)
		assert.Equal(t, "git@github.com:getarcaneapp/arcane.git", source.RepositoryURL)
		assert.Equal(t, "dev", source.Ref)
	})

	t.Run("forge style http url without git suffix", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app")
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, source)
		assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts", source.RepositoryURL)
		assert.Equal(t, "main", source.Ref)
		assert.Equal(t, "docker/app", source.Subdir)
	})

	t.Run("non remote path is ignored", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("./docker/app")
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, source)
	})

	t.Run("invalid subdir traversal is rejected", func(t *testing.T) {
		source, ok, err := ParseGitBuildContextSource("https://github.com/getarcaneapp/arcane.git#main:../secrets")
		require.Error(t, err)
		assert.True(t, ok)
		assert.Nil(t, source)
	})
}

func TestNormalizeGitBuildContextSourceForMatch(t *testing.T) {
	assert.Equal(
		t,
		"https://github.com/getarcaneapp/arcane",
		NormalizeGitBuildContextSourceForMatch("https://github.com/getarcaneapp/arcane.git#main"),
	)
	assert.Equal(
		t,
		"https://github.com/getarcaneapp/arcane",
		NormalizeGitBuildContextSourceForMatch("https://github.com/getarcaneapp/arcane/"),
	)
	assert.Equal(
		t,
		"git@github.com:getarcaneapp/arcane",
		NormalizeGitBuildContextSourceForMatch("git@github.com:getarcaneapp/arcane.git#dev"),
	)
}

func TestRequiresGitRemoteProbe(t *testing.T) {
	assert.True(t, RequiresGitRemoteProbe("https://git.sr.ht/~jordanreger/nws-alerts"))
	assert.False(t, RequiresGitRemoteProbe("https://github.com/getarcaneapp/arcane.git"))
	assert.False(t, RequiresGitRemoteProbe("git@github.com:getarcaneapp/arcane.git"))
	assert.False(t, RequiresGitRemoteProbe("ssh://git@github.com/getarcaneapp/arcane.git"))
}
