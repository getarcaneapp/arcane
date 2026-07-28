package utils

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The whole point of the reference count: releasing a key must not remove an
// entry another goroutine is waiting on, or the two end up on different mutexes
// and both enter the critical section.
func TestKeyedMutexSerializesSameKey(t *testing.T) {
	var locks KeyedMutex

	inside := 0
	maxInside := 0
	var guard sync.Mutex

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			defer locks.Lock("image-1")()

			guard.Lock()
			inside++
			if inside > maxInside {
				maxInside = inside
			}
			guard.Unlock()

			guard.Lock()
			inside--
			guard.Unlock()
		})
	}
	wg.Wait()

	require.Equal(t, 1, maxInside, "two goroutines held the same key at once")
	require.Empty(t, locks.locks, "released keys must not accumulate")
}

func TestKeyedMutexTryLockDoesNotBlockOnHeldKey(t *testing.T) {
	var locks KeyedMutex

	unlock := locks.Lock("a")
	if _, ok := locks.TryLock("a"); ok {
		t.Fatal("TryLock acquired a key that is already held")
	}

	// A different key is unaffected.
	otherUnlock, ok := locks.TryLock("b")
	require.True(t, ok)
	otherUnlock()

	unlock()
	retaken, ok := locks.TryLock("a")
	require.True(t, ok, "key must be acquirable once released")
	retaken()
	require.Empty(t, locks.locks)
}
