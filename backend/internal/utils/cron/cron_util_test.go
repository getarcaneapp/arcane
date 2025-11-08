package cron

import (
	"testing"
)

func TestValidateCronExpression(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "empty expression is valid",
			expr:    "",
			wantErr: false,
		},
		{
			name:    "valid daily at midnight",
			expr:    "0 0 * * *",
			wantErr: false,
		},
		{
			name:    "valid every 6 hours",
			expr:    "0 */6 * * *",
			wantErr: false,
		},
		{
			name:    "valid weekdays at 3am",
			expr:    "0 3 * * 1-5",
			wantErr: false,
		},
		{
			name:    "valid weekends at 2am",
			expr:    "0 2 * * 6,0",
			wantErr: false,
		},
		{
			name:    "invalid - too many fields",
			expr:    "0 0 * * * *",
			wantErr: true,
		},
		{
			name:    "invalid - too few fields",
			expr:    "0 0 *",
			wantErr: true,
		},
		{
			name:    "invalid - bad minute range",
			expr:    "60 0 * * *",
			wantErr: true,
		},
		{
			name:    "invalid - bad hour range",
			expr:    "0 24 * * *",
			wantErr: true,
		},
		{
			name:    "invalid syntax",
			expr:    "invalid cron",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCronExpression(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCronExpression() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
