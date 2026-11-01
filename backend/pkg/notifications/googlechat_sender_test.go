package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGoogleChatURL(t *testing.T) {
	gotURL, err := BuildGoogleChatURL(GoogleChatConfig{
		WebhookURL: "https://chat.googleapis.com/v1/spaces/FOO/messages?key=bar&token=baz",
	})
	require.NoError(t, err)
	assert.Equal(t, "googlechat://chat.googleapis.com/v1/spaces/FOO/messages?key=bar&token=baz", gotURL)
}
