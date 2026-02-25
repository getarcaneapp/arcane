// Package stdcopy is derived from Moby's stdcopy implementation.
//
// Source: https://github.com/moby/moby/blob/v28.5.2/pkg/stdcopy/stdcopy.go
// License: Apache License 2.0
// SPDX-License-Identifier: Apache-2.0
//
// Modified to fix lint errors
// This local copy is vendored to avoid pulling conflicting module variants
// during dependency resolution in this monorepo.
package stdcopy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

// StdType is the type of standard stream
// a writer can multiplex to.
type StdType byte

const (
	// Stdin represents standard input stream type.
	Stdin StdType = iota
	// Stdout represents standard output stream type.
	Stdout
	// Stderr represents standard error steam type.
	Stderr
	// Systemerr represents errors originating from the system that make it
	// into the multiplexed stream.
	Systemerr

	stdWriterPrefixLen = 8
	stdWriterFdIndex   = 0
	stdWriterSizeIndex = 4

	startingBufLen = 32*1024 + stdWriterPrefixLen + 1
)

var bufPool = &sync.Pool{New: func() any { return bytes.NewBuffer(nil) }}

// stdWriter is wrapper of io.Writer with extra customized info.
type stdWriter struct {
	io.Writer
	prefix byte
}

// Write sends the buffer to the underneath writer.
// It inserts the prefix header before the buffer,
// so stdcopy.StdCopy knows where to multiplex the output.
// It makes stdWriter to implement io.Writer.
func (w *stdWriter) Write(p []byte) (int, error) {
	if w == nil || w.Writer == nil {
		return 0, errors.New("writer not instantiated")
	}
	if p == nil {
		return 0, nil
	}
	frameSize := uint64(len(p))
	if frameSize > math.MaxUint32 {
		return 0, fmt.Errorf("message too large for stdcopy frame: %d bytes", frameSize)
	}

	header := [stdWriterPrefixLen]byte{stdWriterFdIndex: w.prefix}
	binary.BigEndian.PutUint32(header[stdWriterSizeIndex:], uint32(frameSize))
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Write(header[:])
	buf.Write(p)

	n, err := w.Writer.Write(buf.Bytes())
	n -= stdWriterPrefixLen
	if n < 0 {
		n = 0
	}

	buf.Reset()
	bufPool.Put(buf)
	return n, err
}

// NewStdWriter instantiates a new Writer.
// Everything written to it will be encapsulated using a custom format,
// and written to the underlying `w` stream.
// This allows multiple write streams (e.g. stdout and stderr) to be muxed into a single connection.
// `t` indicates the id of the stream to encapsulate.
// It can be stdcopy.Stdin, stdcopy.Stdout, stdcopy.Stderr.
func NewStdWriter(w io.Writer, t StdType) io.Writer {
	return &stdWriter{
		Writer: w,
		prefix: byte(t),
	}
}

// StdCopy is a modified version of io.Copy.
//
// StdCopy will demultiplex `src`, assuming that it contains two streams,
// previously multiplexed together using a StdWriter instance.
// As it reads from `src`, StdCopy will write to `dstout` and `dsterr`.
//
// StdCopy will read until it hits EOF on `src`. It will then return a nil error.
// In other words: if `err` is non nil, it indicates a real underlying error.
//
// `written` will hold the total number of bytes written to `dstout` and `dsterr`.
func StdCopy(dstout, dsterr io.Writer, src io.Reader) (written int64, _ error) {
	var (
		buf = make([]byte, startingBufLen)
		nr  int
	)

	for {
		nextNr, err := readUntil(src, buf, nr, stdWriterPrefixLen)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return 0, err
		}
		nr = nextNr

		stream := StdType(buf[stdWriterFdIndex])
		out, err := streamWriter(stream, dstout, dsterr)
		if err != nil {
			return 0, err
		}

		// Retrieve the size of the frame
		frameSize := int(binary.BigEndian.Uint32(buf[stdWriterSizeIndex : stdWriterSizeIndex+4]))

		// Check if the buffer is big enough to read the frame.
		// Extend it if necessary.
		required := frameSize + stdWriterPrefixLen
		if required > len(buf) {
			buf = append(buf, make([]byte, required-len(buf)+1)...)
		}

		nextNr, err = readUntil(src, buf, nr, required)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return 0, err
		}
		nr = nextNr

		// we might have an error from the source mixed up in our multiplexed
		// stream. if we do, return it.
		payload := buf[stdWriterPrefixLen:required]
		if stream == Systemerr {
			return written, fmt.Errorf("error from daemon in stream: %s", string(payload))
		}

		// Write the retrieved frame (without header)
		nw, err := out.Write(payload)
		if err != nil {
			return 0, err
		}

		// If the frame has not been fully written: error
		if nw != frameSize {
			return 0, io.ErrShortWrite
		}
		written += int64(nw)

		// Move the rest of the buffer to the beginning
		copy(buf, buf[required:nr])
		// Move the index
		nr -= required
	}
}

func readUntil(src io.Reader, buf []byte, current, required int) (int, error) {
	n := current
	for n < required {
		nr, err := src.Read(buf[n:])
		n += nr
		if errors.Is(err, io.EOF) {
			if n < required {
				return n, io.EOF
			}
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func streamWriter(stream StdType, dstout, dsterr io.Writer) (io.Writer, error) {
	switch stream {
	case Stdin, Stdout:
		return dstout, nil
	case Stderr:
		return dsterr, nil
	case Systemerr:
		return nil, nil
	default:
		return nil, fmt.Errorf("unrecognized input header: %d", stream)
	}
}
