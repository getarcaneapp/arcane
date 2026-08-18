package notifications

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeliver_UnknownProviderIsNotHandled(t *testing.T) {
	handled, err := Deliver(context.Background(), "bogus", database.JSON{}, Content{})
	require.False(t, handled)
	require.NoError(t, err)
}

func TestProviderDeliverers_CoverAllValidProviders(t *testing.T) {
	for _, provider := range []NotificationProvider{
		NotificationProviderDiscord,
		NotificationProviderEmail,
		NotificationProviderTelegram,
		NotificationProviderSignal,
		NotificationProviderSlack,
		NotificationProviderNtfy,
		NotificationProviderPushover,
		NotificationProviderGotify,
		NotificationProviderMatrix,
		NotificationProviderGeneric,
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
