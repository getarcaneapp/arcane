package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGoogleChatURL(t *testing.T) {
	gotURL, err := BuildGoogleChatURL(models.GoogleChatConfig{
		WebhookURL: "https://chat.googleapis.com/v1/spaces/FOO/messages?key=bar&token=baz",
	})
	require.NoError(t, err)
	assert.Equal(t, "googlechat://chat.googleapis.com/v1/spaces/FOO/messages?key=bar&token=baz", gotURL)
}
