// Copied & modified from https://cs.opensource.google/go/x/tools/+/refs/tags/v0.1.8:godoc/util/util.go
//
// See LICENSE (https://cs.opensource.google/go/x/tools/+/refs/tags/v0.1.8:LICENSE):
// ------------------------------------------------------------------------
// Copyright (c) 2009 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
// * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
// * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
// * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
// ------------------------------------------------------------------------

package scanner

import (
	"io"
	"os"
	"unicode/utf8"
)

const (
	maxBytesUsedToDetermineText = 1024
)

// isTextFile reports whether the first kb of the file looks like correct UTF-8.
// If so it is likely that the file contains human-readable text.
func isTextFile(f *os.File) bool {
	var buf [maxBytesUsedToDetermineText]byte
	n, err := f.Read(buf[0:])
	if err != nil {
		return false
	}

	// rewind reader
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	return isTextString(string(buf[0:n])) // return if the readed string is a text
}

// isTextReader checks if the data in the provided reader looks like correct UTF-8.
// It will consume up to 1KB from the reader to do so.
// Since the reader might not be seekable, the second return value holds the read data and should be used to properly get the whole input stream.
func isTextReader(reader io.Reader) (bool, []byte, error) {
	var buf [maxBytesUsedToDetermineText]byte
	n, err := reader.Read(buf[0:])
	if err != nil {
		return false, nil, err
	}

	// Input might be <1KB
	readData := buf[0:n]
	return isTextString(string(readData)), readData, nil
}

func isTextString(s string) bool {
	for i, c := range s {
		if i+utf8.UTFMax > len(s) {
			// last char may be incomplete - ignore
			break
		}
		if c == 0xFFFD || c < ' ' && c != '\n' && c != '\t' && c != '\f' {
			// decoding error or control character - not a text file
			return false
		}
	}
	return true
}
