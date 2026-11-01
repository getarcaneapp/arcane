package activity

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteErrorLineEncodesControlBytes(t *testing.T) {
	var buf bytes.Buffer
	WriteErrorLine(&buf, errors.New("pull failed \x1b[31mred\x1b[0m"))

	require.Equal(t, byte('\n'), buf.Bytes()[buf.Len()-1])
	var frame map[string]string
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &frame))
	require.Equal(t, "pull failed \x1b[31mred\x1b[0m", frame["error"])
}
