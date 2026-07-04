package notifications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

func TestDeliver_UnknownProviderIsNotHandled(t *testing.T) {
	handled, err := Deliver(context.Background(), models.NotificationProvider("bogus"), models.JSON{}, Content{})
	require.False(t, handled)
	require.NoError(t, err)
}

func TestProviderDeliverers_CoverAllValidProviders(t *testing.T) {
	for _, provider := range []models.NotificationProvider{
		models.NotificationProviderDiscord,
		models.NotificationProviderEmail,
		models.NotificationProviderTelegram,
		models.NotificationProviderSignal,
		models.NotificationProviderSlack,
		models.NotificationProviderNtfy,
		models.NotificationProviderPushover,
		models.NotificationProviderGotify,
		models.NotificationProviderMatrix,
		models.NotificationProviderGeneric,
	} {
		require.Contains(t, providerDeliverers, provider)
	}
}

func TestTextByFormat_BuildsEveryFormat(t *testing.T) {
	text := TextByFormat(func(format MessageFormat) string {
		return "msg:" + string(format)
	})

	require.Len(t, text, 4)
	require.Equal(t, "msg:"+string(MessageFormatMarkdown), text[MessageFormatMarkdown])
	require.Equal(t, "msg:"+string(MessageFormatHTML), text[MessageFormatHTML])
	require.Equal(t, "msg:"+string(MessageFormatSlack), text[MessageFormatSlack])
	require.Equal(t, "msg:"+string(MessageFormatPlain), text[MessageFormatPlain])
}
