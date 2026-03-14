package services

import (
	"testing"
)

func TestDecodeConfigSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{
			name:    "accepts valid spec without labels",
			payload: `{"Name":"app-config","Data":"Y29uZmlnLWRhdGE="}`,
			wantErr: false,
		},
		{
			name:    "rejects empty payload",
			payload: ``,
			wantErr: true,
		},
		{
			name:    "rejects missing name",
			payload: `{"Data":"Y29uZmlnLWRhdGE="}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeConfigSpecInternal([]byte(tc.payload))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestDecodeSecretSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{
			name:    "accepts valid spec without labels",
			payload: `{"Name":"app-secret","Data":"czNjcjN0"}`,
			wantErr: false,
		},
		{
			name:    "rejects empty payload",
			payload: ``,
			wantErr: true,
		},
		{
			name:    "rejects missing name",
			payload: `{"Data":"czNjcjN0"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeSecretSpecInternal([]byte(tc.payload))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
