package cache

import "testing"

func TestKeyedCacheGetSet(t *testing.T) {
	c := NewKeyed[string, *int]()

	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss for unset key")
	}

	v := 42
	c.Set("hit", &v)
	got, ok := c.Get("hit")
	if !ok || got == nil || *got != 42 {
		t.Fatalf("expected cached value 42, got %v (ok=%v)", got, ok)
	}

	// Nil values are valid entries and must report as hits.
	c.Set("nil-entry", nil)
	got, ok = c.Get("nil-entry")
	if !ok || got != nil {
		t.Fatalf("expected nil hit, got %v (ok=%v)", got, ok)
	}

	c.Invalidate("hit")
	if _, ok := c.Get("hit"); ok {
		t.Fatal("expected miss after invalidate")
	}
}
