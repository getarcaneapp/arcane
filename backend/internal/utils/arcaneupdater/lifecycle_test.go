package arcaneupdater

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGetTimeout(t *testing.T) {
	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue time.Duration
		want         time.Duration
	}{
		{
			name:         "no label - use default",
			labels:       map[string]string{},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         60 * time.Second,
		},
		{
			name:         "nil labels - use default",
			labels:       nil,
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         60 * time.Second,
		},
		{
			name:         "valid timeout 30",
			labels:       map[string]string{LabelPreUpdateTimeout: "30"},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         30 * time.Second,
		},
		{
			name:         "valid timeout 120",
			labels:       map[string]string{LabelPostUpdateTimeout: "120"},
			label:        LabelPostUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         120 * time.Second,
		},
		{
			name:         "invalid timeout - use default",
			labels:       map[string]string{LabelPreUpdateTimeout: "invalid"},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         60 * time.Second,
		},
		{
			name:         "negative timeout - use default",
			labels:       map[string]string{LabelPreUpdateTimeout: "-10"},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         60 * time.Second,
		},
		{
			name:         "zero timeout - use default",
			labels:       map[string]string{LabelPreUpdateTimeout: "0"},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         60 * time.Second,
		},
		{
			name:         "timeout with whitespace",
			labels:       map[string]string{LabelPreUpdateTimeout: "  45  "},
			label:        LabelPreUpdateTimeout,
			defaultValue: 60 * time.Second,
			want:         45 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTimeout(tt.labels, tt.label, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLifecycleHookResult_SkipUpdate(t *testing.T) {
	tests := []struct {
		name     string
		result   LifecycleHookResult
		wantSkip bool
	}{
		{
			name: "exit code 75 - should skip",
			result: LifecycleHookResult{
				Executed:   true,
				SkipUpdate: true,
				ExitCode:   ExitCodeSkipUpdate,
			},
			wantSkip: true,
		},
		{
			name: "exit code 0 - should not skip",
			result: LifecycleHookResult{
				Executed:   true,
				SkipUpdate: false,
				ExitCode:   0,
			},
			wantSkip: false,
		},
		{
			name: "not executed - should not skip",
			result: LifecycleHookResult{
				Executed:   false,
				SkipUpdate: false,
			},
			wantSkip: false,
		},
		{
			name: "error occurred - should not skip",
			result: LifecycleHookResult{
				Executed: true,
				ExitCode: 1,
				Error:    errors.New("command failed"),
			},
			wantSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.SkipUpdate; got != tt.wantSkip {
				t.Errorf("LifecycleHookResult.SkipUpdate = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

func TestExecuteLifecycleCommands_NoCommand(t *testing.T) {
	ctx := context.Background()

	// Test with no pre-update command
	result := ExecutePreUpdateCommand(ctx, nil, "test-container", map[string]string{})
	if result.Executed {
		t.Error("ExecutePreUpdateCommand() should not execute when no command is configured")
	}

	// Test with no post-update command
	result = ExecutePostUpdateCommand(ctx, nil, "test-container", map[string]string{})
	if result.Executed {
		t.Error("ExecutePostUpdateCommand() should not execute when no command is configured")
	}

	// Test with no pre-check command
	result = ExecutePreCheckCommand(ctx, nil, "test-container", map[string]string{})
	if result.Executed {
		t.Error("ExecutePreCheckCommand() should not execute when no command is configured")
	}

	// Test with no post-check command
	result = ExecutePostCheckCommand(ctx, nil, "test-container", map[string]string{})
	if result.Executed {
		t.Error("ExecutePostCheckCommand() should not execute when no command is configured")
	}
}

func TestLifecycleCommandOutput(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		wantContains string
	}{
		{
			name:         "output with timestamp",
			output:       "2024-01-01T00:00:00Z Starting backup...",
			wantContains: "Starting backup",
		},
		{
			name:         "multi-line output",
			output:       "Line 1\nLine 2\nLine 3",
			wantContains: "Line 2",
		},
		{
			name:         "empty output",
			output:       "",
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LifecycleHookResult{
				Executed: true,
				Output:   tt.output,
			}
			if tt.wantContains != "" && !strings.Contains(result.Output, tt.wantContains) {
				t.Errorf("Output does not contain expected string: want %q in %q", tt.wantContains, result.Output)
			}
		})
	}
}

func TestDefaultTimeouts(t *testing.T) {
	if DefaultPreUpdateTimeout != 60*time.Second {
		t.Errorf("DefaultPreUpdateTimeout = %v, want %v", DefaultPreUpdateTimeout, 60*time.Second)
	}
	if DefaultPostUpdateTimeout != 60*time.Second {
		t.Errorf("DefaultPostUpdateTimeout = %v, want %v", DefaultPostUpdateTimeout, 60*time.Second)
	}
}

func TestExitCodeSkipUpdate(t *testing.T) {
	if ExitCodeSkipUpdate != 75 {
		t.Errorf("ExitCodeSkipUpdate = %d, want 75", ExitCodeSkipUpdate)
	}
}

func TestLifecycleHookResult_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		result    LifecycleHookResult
		wantError bool
	}{
		{
			name: "no error",
			result: LifecycleHookResult{
				Executed: true,
				ExitCode: 0,
				Error:    nil,
			},
			wantError: false,
		},
		{
			name: "with error",
			result: LifecycleHookResult{
				Executed: true,
				ExitCode: 1,
				Error:    errors.New("command failed"),
			},
			wantError: true,
		},
		{
			name: "not executed no error",
			result: LifecycleHookResult{
				Executed: false,
				Error:    nil,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasError := tt.result.Error != nil
			if hasError != tt.wantError {
				t.Errorf("LifecycleHookResult has error = %v, want %v", hasError, tt.wantError)
			}
		})
	}
}
