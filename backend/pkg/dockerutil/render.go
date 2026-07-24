package docker

import (
	"io"

	"github.com/moby/moby/client/pkg/jsonmessage"
)

// RenderJSONMessageStream renders a Docker JSON message stream (image pull,
// push, build, load) as the raw text the docker CLI prints in non-TTY mode,
// writing it to out. Any daemon-reported error is returned verbatim.
func RenderJSONMessageStream(reader io.Reader, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	return jsonmessage.DisplayJSONMessagesStream(reader, out, 0, false, nil)
}
