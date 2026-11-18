package setupplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

const composeFixture = `
services:
  db:
    image: postgres:${PG_VERSION:-16}
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD:?set DB_PASSWORD}
      POSTGRES_USER: $DB_USER
      LITERAL: "costs $$5 and $${NOT_A_VAR}"
  app:
    image: app
    # ${COMMENTED_OUT} is not a reference
    environment:
      - DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@db/${DB_NAME:-app}
      - FALLBACK=${PRIMARY:-${SECONDARY}}
      - ALT=${FEATURE_FLAG:+on}
    labels:
      ${NOT_INTERPOLATED_KEY}: value
`

func TestComposeVariables(t *testing.T) {
	vars, err := ComposeVariables(composeFixture)
	require.NoError(t, err)

	assert.Equal(t, ComposeVariable{AllDefaulted: true}, vars["PG_VERSION"])
	assert.Equal(t, ComposeVariable{Required: true}, vars["DB_PASSWORD"])
	assert.Equal(t, ComposeVariable{}, vars["DB_USER"])
	assert.Equal(t, ComposeVariable{AllDefaulted: true}, vars["DB_NAME"])
	assert.Equal(t, ComposeVariable{AllDefaulted: true}, vars["PRIMARY"])
	assert.Equal(t, ComposeVariable{AllDefaulted: true}, vars["SECONDARY"])
	assert.Equal(t, ComposeVariable{AllDefaulted: true}, vars["FEATURE_FLAG"])
	for _, absent := range []string{"NOT_A_VAR", "COMMENTED_OUT", "NOT_INTERPOLATED_KEY"} {
		assert.NotContains(t, vars, absent)
	}

	_, err = ComposeVariables("services: [unclosed")
	require.Error(t, err)
}

func TestComposeVariablesMixedReferences(t *testing.T) {
	vars, err := ComposeVariables("x:\n  a: ${TOKEN:-dev}\n  b: ${TOKEN}\n")
	require.NoError(t, err)
	assert.False(t, vars["TOKEN"].AllDefaulted, "one bare reference makes the variable needed")
}

func TestAnalyze(t *testing.T) {
	variables, err := Analyze(Inputs{
		ComposeFiles: []string{composeFixture, "services:\n  app:\n    environment:\n      EXTRA: ${OVERRIDE_ONLY}\n"},
		EnvValues: map[string]string{
			"DB_PASSWORD":  "hunter2",
			"DB_USER":      "app",
			"EMPTY_TOKEN":  "",
			"COMPOSE_FILE": "compose.yaml",
			"TZ":           "America/New_York",
			"bad-key":      "x",
		},
		GitKeys: map[string]struct{}{"TZ": {}},
	})
	require.NoError(t, err)

	byKey := map[string]types.SecretSourceSetupVariable{}
	keys := []string{}
	for _, v := range variables {
		byKey[v.Key] = v
		keys = append(keys, v.Key)
	}
	assert.IsIncreasing(t, keys)
	assert.NotContains(t, byKey, "COMPOSE_FILE")
	assert.NotContains(t, byKey, "bad-key")

	assert.Equal(t, types.SecretSourceSetupVariable{
		Key: "DB_PASSWORD", InCompose: true, ComposeRequired: true, InEnvFile: true, SecretLike: true,
		Remote: types.SecretSourceSetupRemoteUnknown,
	}, byKey["DB_PASSWORD"])
	assert.True(t, byKey["EMPTY_TOKEN"].EnvEmpty)
	assert.False(t, byKey["EMPTY_TOKEN"].InCompose)
	assert.True(t, byKey["TZ"].FromGit)
	assert.True(t, byKey["OVERRIDE_ONLY"].InCompose)
	assert.False(t, byKey["OVERRIDE_ONLY"].InEnvFile)
	assert.True(t, byKey["PG_VERSION"].ComposeDefault)
}

func TestIsSecretLike(t *testing.T) {
	for _, key := range []string{"DB_PASSWORD", "GITHUB_TOKEN", "APP_KEY", "SMTP_PASS", "JWT_SECRET", "SENTRY_DSN", "OAUTH_CLIENT_SECRET", "api_key"} {
		assert.True(t, IsSecretLike(key), key)
	}
	for _, key := range []string{"TZ", "PUID", "DB_HOST", "DB_PASSWORD_FILE", "SSH_KEY_PATH", "KEYBOARD_LAYOUT"} {
		assert.False(t, IsSecretLike(key), key)
	}
}

func TestRemoveEnvKeys(t *testing.T) {
	content := `# database
DB_USER=app
DB_PASSWORD="multi
line \" still inside
end"
export API_TOKEN=abc # inline comment
TZ=America/New_York
CERT='-----BEGIN-----
abc
-----END-----'
YAML_STYLE: value

`
	updated, removed := RemoveEnvKeys(content, []string{"DB_PASSWORD", "API_TOKEN", "CERT", "YAML_STYLE", "NOT_THERE"})
	assert.Equal(t, []string{"API_TOKEN", "CERT", "DB_PASSWORD", "YAML_STYLE"}, removed)
	assert.Equal(t, "# database\nDB_USER=app\nTZ=America/New_York\n\n", updated)

	same, none := RemoveEnvKeys("A=1\nB=2", nil)
	assert.Equal(t, "A=1\nB=2", same)
	assert.Empty(t, none)

	// An unterminated quote swallows the rest, like a dotenv parser would.
	cut, gone := RemoveEnvKeys("A=1\nB=\"open\nC=3", []string{"B"})
	assert.Equal(t, "A=1", cut)
	assert.Equal(t, []string{"B"}, gone)
}

func TestPlanEnvRemovalKeepsReferencedKeys(t *testing.T) {
	content := "DB_PASSWORD=hunter2\nDB_URL=postgres://app:${DB_PASSWORD}@db/app\nAPI_TOKEN=abc\nSALT=x\nPEPPER=\"$SALT and\nmore\"\n"
	updated, removed, referenced := PlanEnvRemoval(content, []string{"DB_PASSWORD", "API_TOKEN", "SALT"})
	assert.Equal(t, []string{"API_TOKEN"}, removed)
	assert.Equal(t, []string{"DB_PASSWORD", "SALT"}, referenced)
	assert.Equal(t, "DB_PASSWORD=hunter2\nDB_URL=postgres://app:${DB_PASSWORD}@db/app\nSALT=x\nPEPPER=\"$SALT and\nmore\"\n", updated)

	// Removing the referencing entry too frees the referenced key.
	_, removed, referenced = PlanEnvRemoval(content, []string{"DB_PASSWORD", "DB_URL"})
	assert.Equal(t, []string{"DB_PASSWORD", "DB_URL"}, removed)
	assert.Empty(t, referenced)
}

func TestReferencedKeys(t *testing.T) {
	refs := ReferencedKeys("# ${COMMENT}\nA=${B}\nC='$D'\nE=$${ESCAPED}\nF=plain\n")
	assert.Equal(t, map[string]struct{}{"B": {}, "D": {}}, refs)
}
