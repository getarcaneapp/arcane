package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeBrowsePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "current directory is root", input: ".", want: "/"},
		{name: "parent traversal is rejected", input: "..", wantErr: true},
		{name: "root stays root", input: "/", want: "/"},
		{name: "empty is root", input: "", want: "/"},
		{name: "relative path is rooted", input: "a/b", want: "/a/b"},
		{name: "absolute path is cleaned", input: "/a/../b", want: "/b"},
		{name: "escaping relative traversal is rejected", input: "a/../../../etc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeBrowsePath(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
