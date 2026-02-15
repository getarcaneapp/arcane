package libbuild

import (
	"encoding/json"
	"io"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
)

type flusher interface{ Flush() }

func writeProgressEvent(w io.Writer, event imagetypes.ProgressEvent) {
	if w == nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
	if f, ok := w.(flusher); ok {
		f.Flush()
	}
}
