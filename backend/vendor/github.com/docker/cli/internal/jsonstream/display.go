package jsonstream

import (
	"context"
	"io"

	"github.com/docker/docker/pkg/jsonmessage"
)

type (
	Stream       = jsonmessage.Stream
	JSONMessage  = jsonmessage.JSONMessage
	JSONError    = jsonmessage.JSONError
	JSONProgress = jsonmessage.JSONProgress
)

type ctxReader struct {
	err chan error
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (n int, err error) {
	select {
	case err = <-r.err:
		return 0, err
	default:
		return r.r.Read(p)
	}
}

type Options func(*options)

type options struct {
	AuxCallback func(JSONMessage)
}

func WithAuxCallback(cb func(JSONMessage)) Options {
	return func(o *options) {
		o.AuxCallback = cb
	}
}

// Display prints the JSON messages from the given reader to the given stream.
//
// It wraps the [jsonmessage.DisplayJSONMessagesStream] function to make it
// "context aware" and appropriately returns why the function was canceled.
//
// It returns an error if the context is canceled, but not if the input reader / stream is closed.
func Display(ctx context.Context, in io.Reader, stream Stream, opts ...Options) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	reader := &ctxReader{err: make(chan error, 1), r: in}
	stopFunc := context.AfterFunc(ctx, func() { reader.err <- ctx.Err() })
	defer stopFunc()

	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	if err := jsonmessage.DisplayJSONMessagesStream(reader, stream, stream.FD(), stream.IsTerminal(), o.AuxCallback); err != nil {
		return err
	}

	return ctx.Err()
}
