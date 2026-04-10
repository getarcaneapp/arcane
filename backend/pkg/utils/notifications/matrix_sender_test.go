package notifications

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMatrixURL(t *testing.T) {
	tests := []struct {
		name     string
		config   models.MatrixConfig
		wantErr  bool
		expected string
	}{
		{
			name: "basic config (host + token)",
			config: models.MatrixConfig{
				Host:     "matrix.example.com",
				Password: "t0ken",
			},
			wantErr:  false,
			expected: "matrix://:t0ken@matrix.example.com",
		},
		{
			name: "config with host + token + room",
			config: models.MatrixConfig{
				Host:     "matrix.example.com",
				Password: "t0ken",
				Rooms:    "!roomId",
			},
			wantErr:  false,
			expected: "matrix://:t0ken@matrix.example.com?rooms=!roomId",
		},
		{
			name: "config with host + username + password + room",
			config: models.MatrixConfig{
				Host:     "matrix.example.com",
				Username: "A12345678901234",
				Password: "passw0rd",
				Rooms:    "!roomId",
			},
			wantErr:  false,
			expected: "matrix://A12345678901234:passw0rd@matrix.example.com?rooms=!roomId",
		},
		{
			name: "config with host + port + username + password + room",
			config: models.MatrixConfig{
				Host:     "matrix.example.com",
				Port:     8443,
				Username: "A12345678901234",
				Password: "passw0rd",
				Rooms:    "!roomId",
			},
			wantErr:  false,
			expected: "matrix://A12345678901234:passw0rd@matrix.example.com:8443?rooms=!roomId",
		},
		{
			name: "config with all options",
			config: models.MatrixConfig{
				Host:                   "matrix.example.com",
				Port:                   8443,
				Username:               "A12345678901234",
				Password:               "passw0rd",
				Rooms:                  "!room1",
				DisableTLSVerification: true,
			},
			wantErr:  false,
			expected: "matrix://A12345678901234:passw0rd@matrix.example.com:8443?disabletls=yes&rooms=!room1",
		},
		{
			name: "missing host",
			config: models.MatrixConfig{
				Username: "A12345678901234",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := BuildMatrixURL(tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, gotURL)
			}
		})
	}
}
