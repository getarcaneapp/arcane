package httpx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOutboundHTTPURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "allow https URL", rawURL: "https://example.com/api/health"},
		{name: "allow private network URL", rawURL: "http://10.0.0.5:3553/api/health"},
		{name: "reject empty URL", rawURL: "", wantErr: true},
		{name: "reject unsupported scheme", rawURL: "ftp://example.com", wantErr: true},
		{name: "reject missing host", rawURL: "https:///api/health", wantErr: true},
		{name: "reject embedded credentials", rawURL: "https://user:pass@example.com/api/health", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ValidateOutboundHTTPURL(tt.rawURL)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
		})
	}
}
