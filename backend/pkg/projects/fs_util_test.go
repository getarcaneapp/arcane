package projects

import "testing"

func TestResolveConfiguredContainerDirectory(t *testing.T) {
	t.Run("uses default when empty", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("", "/app/data/swarm/sources")
		if got != "/app/data/swarm/sources" {
			t.Fatalf("expected default path, got %q", got)
		}
	})

	t.Run("preserves plain absolute path", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("/app/data/custom/stacks", "/app/data/swarm/sources")
		if got != "/app/data/custom/stacks" {
			t.Fatalf("expected plain absolute path, got %q", got)
		}
	})

	t.Run("extracts container path from bind mapping", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("/app/data/swarm/sources:/srv/arcane/swarm", "/app/data/swarm/sources")
		if got != "/app/data/swarm/sources" {
			t.Fatalf("expected container-side path, got %q", got)
		}
	})

	t.Run("normalizes relative path", func(t *testing.T) {
		got := ResolveConfiguredContainerDirectory("data/swarm/sources", "/app/data/swarm/sources")
		if got == "data/swarm/sources" {
			t.Fatalf("expected absolute normalized path, got %q", got)
		}
	})
}
