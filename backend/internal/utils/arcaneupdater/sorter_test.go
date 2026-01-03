package arcaneupdater

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestContainerSorter_Sort(t *testing.T) {
	tests := []struct {
		name       string
		containers []ContainerWithDeps
		want       []string // Expected order by name
		wantErr    bool
	}{
		{
			name:       "empty containers",
			containers: []ContainerWithDeps{},
			want:       []string{},
			wantErr:    false,
		},
		{
			name: "single container no deps",
			containers: []ContainerWithDeps{
				{Name: "app", Container: container.Summary{ID: "app1"}},
			},
			want:    []string{"app"},
			wantErr: false,
		},
		{
			name: "two containers no deps",
			containers: []ContainerWithDeps{
				{Name: "app", Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			want:    []string{"app", "db"},
			wantErr: false,
		},
		{
			name: "simple dependency - app depends on db",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			want:    []string{"db", "app"},
			wantErr: false,
		},
		{
			name: "link dependency - app links to db",
			containers: []ContainerWithDeps{
				{Name: "app", Links: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			want:    []string{"db", "app"},
			wantErr: false,
		},
		{
			name: "network dependency - app uses db network",
			containers: []ContainerWithDeps{
				{Name: "app", NetworkDeps: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			want:    []string{"db", "app"},
			wantErr: false,
		},
		{
			name: "chain dependency - app -> db -> cache",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", DependsOn: []string{"cache"}, Container: container.Summary{ID: "db1"}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			want:    []string{"cache", "db", "app"},
			wantErr: false,
		},
		{
			name: "multiple dependencies - app depends on db and cache",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db", "cache"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			want:    []string{"db", "cache", "app"},
			wantErr: false,
		},
		{
			name: "diamond dependency - app and worker depend on db, both depend on cache",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "worker", DependsOn: []string{"db"}, Container: container.Summary{ID: "worker1"}},
				{Name: "db", DependsOn: []string{"cache"}, Container: container.Summary{ID: "db1"}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			want:    []string{"cache", "db", "app", "worker"},
			wantErr: false,
		},
		{
			name: "circular dependency - app -> db -> app",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", DependsOn: []string{"app"}, Container: container.Summary{ID: "db1"}},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "circular dependency chain - app -> db -> cache -> app",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", DependsOn: []string{"cache"}, Container: container.Summary{ID: "db1"}},
				{Name: "cache", DependsOn: []string{"app"}, Container: container.Summary{ID: "cache1"}},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "missing dependency - app depends on nonexistent db",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
			},
			want:    []string{"app"},
			wantErr: false,
		},
		{
			name: "mixed dependency types - links, depends-on, network",
			containers: []ContainerWithDeps{
				{Name: "app", Links: []string{"db"}, NetworkDeps: []string{"proxy"}, Container: container.Summary{ID: "app1"}},
				{Name: "worker", DependsOn: []string{"db"}, Container: container.Summary{ID: "worker1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
				{Name: "proxy", Container: container.Summary{ID: "proxy1"}},
			},
			want:    []string{"db", "proxy", "app", "worker"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewContainerSorter(tt.containers)
			got, err := s.Sort()

			if (err != nil) != tt.wantErr {
				t.Errorf("Sort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("Sort() returned %d containers, want %d", len(got), len(tt.want))
				return
			}

			for i, c := range got {
				if c.Name != tt.want[i] {
					t.Errorf("Sort()[%d].Name = %v, want %v", i, c.Name, tt.want[i])
				}
			}
		})
	}
}

func TestContainerSorter_SortReverse(t *testing.T) {
	tests := []struct {
		name       string
		containers []ContainerWithDeps
		want       []string // Expected reverse order by name
		wantErr    bool
	}{
		{
			name:       "empty containers",
			containers: []ContainerWithDeps{},
			want:       []string{},
			wantErr:    false,
		},
		{
			name: "simple dependency - reverse should be app then db",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			want:    []string{"app", "db"},
			wantErr: false,
		},
		{
			name: "chain dependency - reverse should be app -> db -> cache",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", DependsOn: []string{"cache"}, Container: container.Summary{ID: "db1"}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			want:    []string{"app", "db", "cache"},
			wantErr: false,
		},
		{
			name: "circular dependency",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", DependsOn: []string{"app"}, Container: container.Summary{ID: "db1"}},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewContainerSorter(tt.containers)
			got, err := s.SortReverse()

			if (err != nil) != tt.wantErr {
				t.Errorf("SortReverse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("SortReverse() returned %d containers, want %d", len(got), len(tt.want))
				return
			}

			for i, c := range got {
				if c.Name != tt.want[i] {
					t.Errorf("SortReverse()[%d].Name = %v, want %v", i, c.Name, tt.want[i])
				}
			}
		})
	}
}

func TestUpdateImplicitRestart(t *testing.T) {
	tests := []struct {
		name             string
		containers       []ContainerWithDeps
		markedForRestart map[string]bool
		wantImplicit     []string
		wantMarkedAfter  map[string]bool
	}{
		{
			name:             "no containers",
			containers:       []ContainerWithDeps{},
			markedForRestart: map[string]bool{},
			wantImplicit:     []string{},
			wantMarkedAfter:  map[string]bool{},
		},
		{
			name: "no dependencies, no restarts",
			containers: []ContainerWithDeps{
				{Name: "app", Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			markedForRestart: map[string]bool{},
			wantImplicit:     []string{},
			wantMarkedAfter:  map[string]bool{},
		},
		{
			name: "db marked for restart, app depends on db",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1", Labels: map[string]string{}}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			markedForRestart: map[string]bool{"db": true},
			wantImplicit:     []string{"app"},
			wantMarkedAfter:  map[string]bool{"db": true, "app": true},
		},
		{
			name: "chain reaction - cache restart triggers db (not app in single pass)",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1", Labels: map[string]string{}}},
				{Name: "db", DependsOn: []string{"cache"}, Container: container.Summary{ID: "db1", Labels: map[string]string{}}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			markedForRestart: map[string]bool{"cache": true},
			wantImplicit:     []string{"db"}, // Only direct dependencies in single pass
			wantMarkedAfter:  map[string]bool{"cache": true, "db": true},
		},
		{
			name: "app already marked, should not be in implicit list",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db"}, Container: container.Summary{ID: "app1"}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			markedForRestart: map[string]bool{"db": true, "app": true},
			wantImplicit:     []string{},
			wantMarkedAfter:  map[string]bool{"db": true, "app": true},
		},
		{
			name: "link dependency triggers restart",
			containers: []ContainerWithDeps{
				{Name: "app", Links: []string{"db"}, Container: container.Summary{ID: "app1", Labels: map[string]string{}}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			markedForRestart: map[string]bool{"db": true},
			wantImplicit:     []string{"app"},
			wantMarkedAfter:  map[string]bool{"db": true, "app": true},
		},
		{
			name: "network dependency triggers restart",
			containers: []ContainerWithDeps{
				{Name: "app", NetworkDeps: []string{"db"}, Container: container.Summary{ID: "app1", Labels: map[string]string{}}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
			},
			markedForRestart: map[string]bool{"db": true},
			wantImplicit:     []string{"app"},
			wantMarkedAfter:  map[string]bool{"db": true, "app": true},
		},
		{
			name: "multiple dependencies - one triggers restart",
			containers: []ContainerWithDeps{
				{Name: "app", DependsOn: []string{"db", "cache"}, Container: container.Summary{ID: "app1", Labels: map[string]string{}}},
				{Name: "db", Container: container.Summary{ID: "db1"}},
				{Name: "cache", Container: container.Summary{ID: "cache1"}},
			},
			markedForRestart: map[string]bool{"db": true},
			wantImplicit:     []string{"app"},
			wantMarkedAfter:  map[string]bool{"db": true, "app": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpdateImplicitRestart(tt.containers, tt.markedForRestart)

			if len(got) != len(tt.wantImplicit) {
				t.Errorf("UpdateImplicitRestart() returned %d implicit restarts, want %d", len(got), len(tt.wantImplicit))
				t.Errorf("Got: %v, Want: %v", got, tt.wantImplicit)
			}

			// Check implicit restart list
			implicitMap := make(map[string]bool)
			for _, name := range got {
				implicitMap[name] = true
			}
			for _, want := range tt.wantImplicit {
				if !implicitMap[want] {
					t.Errorf("UpdateImplicitRestart() missing implicit restart for %v", want)
				}
			}

			// Check markedForRestart map after update
			for name, want := range tt.wantMarkedAfter {
				if got := tt.markedForRestart[name]; got != want {
					t.Errorf("After UpdateImplicitRestart(), markedForRestart[%v] = %v, want %v", name, got, want)
				}
			}
		})
	}
}

func TestExtractContainerName(t *testing.T) {
	tests := []struct {
		name string
		cnt  container.Summary
		want string
	}{
		{
			name: "single name with slash",
			cnt:  container.Summary{Names: []string{"/myapp"}, ID: "abc123"},
			want: "myapp",
		},
		{
			name: "single name without slash",
			cnt:  container.Summary{Names: []string{"myapp"}, ID: "abc123"},
			want: "myapp",
		},
		{
			name: "multiple names - uses first",
			cnt:  container.Summary{Names: []string{"/myapp", "/myapp-alias"}, ID: "abc123"},
			want: "myapp",
		},
		{
			name: "no names - falls back to short ID",
			cnt:  container.Summary{Names: []string{}, ID: "abc123456789"},
			want: "abc123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractContainerName(tt.cnt); got != tt.want {
				t.Errorf("ExtractContainerName() = %v, want %v", got, tt.want)
			}
		})
	}
}
