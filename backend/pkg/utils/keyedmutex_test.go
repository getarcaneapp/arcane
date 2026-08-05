package utils

import (
	"sync"
	"testing"
	"time"

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
	{
		_, ok := locks.TryLock("a")
		require.False(t, ok,
			"TryLock acquired a key that is already held")
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

func TestKeyedMutexReadLocksShareKeyAndBlockWriter(t *testing.T) {
	var locks KeyedMutex

	firstReadUnlock := locks.RLock("volume")
	secondReadUnlock := locks.RLock("volume")
	writerAcquired := make(chan struct{})
	writerReleased := make(chan struct{})
	go func() {
		defer close(writerReleased)
		defer locks.Lock("volume")()
		close(writerAcquired)
	}()

	require.Never(t, func() bool {
		select {
		case <-writerAcquired:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond, "writer acquired a key while readers held it")

	firstReadUnlock()
	require.Never(t, func() bool {
		select {
		case <-writerAcquired:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond, "writer acquired a key before every reader released it")

	secondReadUnlock()
	require.Eventually(t, func() bool {
		select {
		case <-writerAcquired:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	<-writerReleased
	require.Empty(t, locks.locks)
}
