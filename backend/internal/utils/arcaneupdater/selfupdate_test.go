package arcaneupdater

import (
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

func TestGenerateTempName(t *testing.T) {
	names := make(map[string]bool)

	for i := 0; i < 10; i++ {
		name := generateTempName()

		if !strings.HasPrefix(name, "arcane-old-") {
			t.Errorf("generateTempName() = %q, want prefix 'arcane-old-'", name)
		}

		if len(name) != 19 {
			t.Errorf("generateTempName() length = %d, want 19", len(name))
		}

		// Check for valid alphanumeric chars
		suffix := strings.TrimPrefix(name, "arcane-old-")
		for _, c := range suffix {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				t.Errorf("generateTempName() suffix contains invalid char: %c in %q", c, suffix)
			}
		}

		names[name] = true
	}

	// Should have unique names
	if len(names) < 9 {
		t.Errorf("generateTempName() not unique enough: %d unique out of 10", len(names))
	}
}

func TestSortByCreated(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		containers []container.Summary
		want       []string // Expected order (newest first)
	}{
		{
			name:       "empty list",
			containers: []container.Summary{},
			want:       []string{},
		},
		{
			name: "single container",
			containers: []container.Summary{
				{ID: "abc", Created: now.Unix()},
			},
			want: []string{"abc"},
		},
		{
			name: "two containers - newest first",
			containers: []container.Summary{
				{ID: "older", Created: now.Add(-1 * time.Hour).Unix()},
				{ID: "newer", Created: now.Unix()},
			},
			want: []string{"newer", "older"},
		},
		{
			name: "three containers - newest first",
			containers: []container.Summary{
				{ID: "oldest", Created: now.Add(-2 * time.Hour).Unix()},
				{ID: "middle", Created: now.Add(-1 * time.Hour).Unix()},
				{ID: "newest", Created: now.Unix()},
			},
			want: []string{"newest", "middle", "oldest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containers := make([]container.Summary, len(tt.containers))
			copy(containers, tt.containers)

			sortByCreated(containers)

			if len(containers) != len(tt.want) {
				t.Errorf("sortByCreated() len = %d, want %d", len(containers), len(tt.want))
				return
			}

			for i, want := range tt.want {
				if containers[i].ID != want {
					t.Errorf("sortByCreated()[%d].ID = %q, want %q", i, containers[i].ID, want)
				}
			}
		})
	}
}

func TestNewSelfUpdate(t *testing.T) {
	su := NewSelfUpdate(nil)

	// NewSelfUpdate always returns a valid instance
	if su.dcli != nil {
		t.Error("NewSelfUpdate(nil) should set dcli to nil")
	}
}

func TestGenerateTempName_ValidDockerName(t *testing.T) {
	name := generateTempName()

	// Docker container names must match [a-zA-Z0-9][a-zA-Z0-9_.-]*
	for i, c := range name {
		valid := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.'

		if !valid {
			t.Errorf("generateTempName()[%d] = %c is not valid for Docker container name", i, c)
		}
	}
}

func TestSortByCreated_SameTimestamp(t *testing.T) {
	now := time.Now().Unix()

	containers := []container.Summary{
		{ID: "first", Created: now},
		{ID: "second", Created: now},
		{ID: "third", Created: now},
	}

	sortByCreated(containers)

	// Order should be preserved since all have same timestamp
	if containers[0].ID != "first" || containers[1].ID != "second" || containers[2].ID != "third" {
		t.Errorf("sortByCreated() not stable for equal elements")
	}
}
