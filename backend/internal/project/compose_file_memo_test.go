package project

import (
	"errors"
	"testing"
)

func TestComposeFileInternal(t *testing.T) {
	t.Parallel()

	const resolved = "/projects/app/compose.yaml"
	transient := errors.New("transient")

	type call struct {
		wantPath  string
		wantErr   error
		wantCalls int
	}

	tests := []struct {
		name      string
		env       *projectMetadataEnvInternal
		projectID string
		// results is returned by resolve in order; the last entry repeats.
		results []error
		calls   []call
	}{
		{
			name:      "caches success",
			env:       &projectMetadataEnvInternal{},
			projectID: "p1",
			results:   []error{nil},
			calls: []call{
				{wantPath: resolved, wantCalls: 1},
				{wantPath: resolved, wantCalls: 1},
			},
		},
		{
			name:      "retries after failure then caches success",
			env:       &projectMetadataEnvInternal{},
			projectID: "p1",
			results:   []error{transient, nil},
			calls: []call{
				{wantErr: transient, wantCalls: 1},
				{wantPath: resolved, wantCalls: 2},
				{wantPath: resolved, wantCalls: 2},
			},
		},
		{
			name:      "repeated failures are never memoized",
			env:       &projectMetadataEnvInternal{},
			projectID: "p1",
			results:   []error{transient},
			calls: []call{
				{wantErr: transient, wantCalls: 1},
				{wantErr: transient, wantCalls: 2},
			},
		},
		{
			name:      "nil env resolves every call",
			env:       nil,
			projectID: "p1",
			results:   []error{nil},
			calls: []call{
				{wantPath: resolved, wantCalls: 1},
				{wantPath: resolved, wantCalls: 2},
			},
		},
		{
			name:      "empty project ID resolves every call",
			env:       &projectMetadataEnvInternal{},
			projectID: "",
			results:   []error{nil},
			calls: []call{
				{wantPath: resolved, wantCalls: 1},
				{wantPath: resolved, wantCalls: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			resolve := func() (string, error) {
				calls++
				err := tt.results[min(calls, len(tt.results))-1]
				if err != nil {
					return "", err
				}
				return resolved, nil
			}

			for i, want := range tt.calls {
				path, err := tt.env.composeFileInternal(tt.projectID, resolve)
				if !errors.Is(err, want.wantErr) {
					t.Fatalf("call %d: err = %v, want %v", i+1, err, want.wantErr)
				}
				if path != want.wantPath {
					t.Fatalf("call %d: path = %q, want %q", i+1, path, want.wantPath)
				}
				if calls != want.wantCalls {
					t.Fatalf("call %d: resolve invoked %d times, want %d", i+1, calls, want.wantCalls)
				}
			}
		})
	}
}
