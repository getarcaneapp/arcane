package activity

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	activitytypes "github.com/getarcaneapp/arcane/types/activity"
	"github.com/stretchr/testify/require"
)

type recordingAppender struct {
	messages []AppendMessageRequest
}

func (a *recordingAppender) AppendMessage(_ context.Context, _ string, req AppendMessageRequest) (*activitytypes.Message, error) {
	a.messages = append(a.messages, req)
	return &activitytypes.Message{}, nil
}

type failingWriter struct{}

func (f failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestWriterContinuesActivityCaptureWhenResponseWriterFailsInternal(t *testing.T) {
	appender := &recordingAppender{}
	writer := NewWriter(context.Background(), appender, "activity-1", failingWriter{}, "Pulling image")

	n, err := writer.Write([]byte("Downloading layer\n"))
	require.NoError(t, err)
	require.Equal(t, len("Downloading layer\n"), n)

	FlushWriter(writer)
	require.Eventually(t, func() bool {
		return len(appender.messages) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, "Downloading layer", appender.messages[0].Message)
	require.Equal(t, models.ActivityMessageLevelInfo, appender.messages[0].Level)
}

func TestWriterReturnsWrappedWriteErrorWithoutActivityInternal(t *testing.T) {
	writer := NewWriter(context.Background(), nil, "", failingWriter{}, "Pulling image")

	_, err := writer.Write([]byte("Downloading layer\n"))
	require.ErrorContains(t, err, "client disconnected")
}

var _ MessageAppender = (*recordingAppender)(nil)
var _ io.Writer = failingWriter{}
