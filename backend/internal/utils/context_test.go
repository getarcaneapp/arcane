package utils

import (
	"context"
	"testing"
	"time"
)

func TestDeriveContext_InheritAndIndependent(t *testing.T) {
	t.Run("inherit cancellation", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		child, cancelChild := DeriveContext(parent, 0, false)
		defer cancelChild()

		// cancel parent -> child should be canceled
		cancelParent()

		select {
		case <-child.Done():
			// ok
		case <-time.After(50 * time.Millisecond):
			t.Fatalf("child context was not canceled when parent canceled")
		}
	})

	t.Run("independent not canceled by parent", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		child, cancelChild := DeriveContext(parent, 0, true)
		defer cancelChild()

		// cancel parent -> child should NOT be canceled
		cancelParent()

		select {
		case <-child.Done():
			t.Fatalf("independent child context was canceled when parent canceled")
		case <-time.After(20 * time.Millisecond):
			// ok
		}
	})
}

func TestDeriveContext_WithTimeout(t *testing.T) {
	child, cancel := DeriveContext(context.Background(), 30*time.Millisecond, false)
	defer cancel()

	// Should have a deadline set
	if _, ok := child.Deadline(); !ok {
		t.Fatalf("expected deadline on child context with timeout")
	}

	// Wait for it to be canceled by timeout
	select {
	case <-child.Done():
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("child context did not time out as expected")
	}
}

func TestDeriveContextOptional_NoTimeout_NoopCancel(t *testing.T) {
	ctx := context.Background()
	child, cancel := DeriveContextOptional(ctx, nil, false)
	// cancel should be safe to call (noop) and not panic
	cancel()

	// Ensure returned context is not nil; don't assume Done() is non-nil
	if child == nil {
		t.Fatalf("expected non-nil context")
	}
}
