package setupplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refsComposeFixture = `# My stack
services:
  db:
    image: postgres:16
    # credentials
    environment:
      POSTGRES_USER: app # inline comment
      POSTGRES_DB: app

    volumes:
      - db:/var/lib/postgresql/data
  app:
    image: app:latest
    environment:
      - DATABASE_URL=postgres://app:${DB_PASSWORD}@db/app
      - TZ
    command: >
      serve
      --port 8080
  worker:
    image: app:latest
  inline:
    image: x
    environment: {A: b}
  empty:
    image: y
    environment:

volumes:
  db: {}
`

func TestComposeServices(t *testing.T) {
	services, err := ComposeServices(refsComposeFixture)
	require.NoError(t, err)
	require.Len(t, services, 5)
	assert.Equal(t, ComposeService{Name: "db", Available: []string{"POSTGRES_DB", "POSTGRES_USER"}, Editable: true}, services[0])
	assert.Equal(t, []string{"DATABASE_URL", "DB_PASSWORD", "TZ"}, services[1].Available)
	assert.Equal(t, "worker", services[2].Name)
	assert.Empty(t, services[2].Available)
	assert.False(t, services[3].Editable)
	assert.NotEmpty(t, services[3].Reason)
	assert.True(t, services[4].Editable)

	_, err = ComposeServices("version: '3'\n")
	require.Error(t, err)
	_, err = ComposeServices("services: [")
	require.Error(t, err)
}

func TestInjectEnvironmentRefs(t *testing.T) {
	updated, added, skipped, err := InjectEnvironmentRefs(refsComposeFixture, map[string][]string{
		"db":      {"POSTGRES_PASSWORD", "POSTGRES_USER"},
		"app":     {"API_TOKEN", "DB_PASSWORD"},
		"worker":  {"API_TOKEN", "QUEUE_PASSWORD"},
		"inline":  {"X"},
		"empty":   {"E"},
		"missing": {"Y"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"db":     {"POSTGRES_PASSWORD"},
		"app":    {"API_TOKEN"},
		"worker": {"API_TOKEN", "QUEUE_PASSWORD"},
		"empty":  {"E"},
	}, added)
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Service+"/"+s.Key] = s.Reason
	}
	assert.Equal(t, "already available to the service", reasons["db/POSTGRES_USER"])
	assert.Equal(t, "already available to the service", reasons["app/DB_PASSWORD"])
	assert.Contains(t, reasons["inline/X"], "inline")
	assert.Equal(t, "no such service", reasons["missing/Y"])

	assert.Equal(t, `# My stack
services:
  db:
    image: postgres:16
    # credentials
    environment:
      POSTGRES_USER: app # inline comment
      POSTGRES_DB: app
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}

    volumes:
      - db:/var/lib/postgresql/data
  app:
    image: app:latest
    environment:
      - DATABASE_URL=postgres://app:${DB_PASSWORD}@db/app
      - TZ
      - API_TOKEN=${API_TOKEN}
    command: >
      serve
      --port 8080
  worker:
    environment:
      API_TOKEN: ${API_TOKEN}
      QUEUE_PASSWORD: ${QUEUE_PASSWORD}
    image: app:latest
  inline:
    image: x
    environment: {A: b}
  empty:
    image: y
    environment:
      E: ${E}

volumes:
  db: {}
`, updated)

	// Running it again adds nothing.
	again, added, _, err := InjectEnvironmentRefs(updated, map[string][]string{"worker": {"API_TOKEN"}})
	require.NoError(t, err)
	assert.Empty(t, added)
	assert.Equal(t, updated, again)
}

func TestInjectEnvironmentRefsFourSpaceIndent(t *testing.T) {
	content := "services:\n    web:\n        image: nginx\n"
	updated, _, _, err := InjectEnvironmentRefs(content, map[string][]string{"web": {"TOKEN"}})
	require.NoError(t, err)
	assert.Equal(t, "services:\n    web:\n        environment:\n            TOKEN: ${TOKEN}\n        image: nginx\n", updated)
}
