package utils

import "sync"

// KeyedMutex hands out one mutex per key, so unrelated keys proceed
// concurrently while work on the same key is serialized.
//
// Entries are reference counted and dropped once nobody holds or awaits them.
// The naive version of this — a sync.Map of *sync.Mutex — either grows one
// entry per key ever seen, or, if entries are deleted on release, hands two
// callers different mutexes for the same key and silently loses mutual
// exclusion. The reference count is taken under the map lock, so a key with
// waiters is never removed.
//
// The zero value is ready to use.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedMutexEntry
}

type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

// reserve returns the entry for key with its reference count incremented.
func (k *KeyedMutex) reserve(key string) *keyedMutexEntry {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.locks == nil {
		k.locks = make(map[string]*keyedMutexEntry)
	}
	entry, ok := k.locks[key]
	if !ok {
		entry = &keyedMutexEntry{}
		k.locks[key] = entry
	}
	entry.refs++
	return entry
}

// release drops one reference and removes the entry when it reaches zero.
func (k *KeyedMutex) release(key string, entry *keyedMutexEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()

	entry.refs--
	if entry.refs == 0 && k.locks[key] == entry {
		delete(k.locks, key)
	}
}

// Lock blocks until the key's mutex is held and returns its unlock function.
// Intended to be deferred at the point of acquisition:
//
//	defer locks.Lock(id)()
func (k *KeyedMutex) Lock(key string) func() {
	entry := k.reserve(key)
	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()
		k.release(key, entry)
	}
}

// TryLock acquires the key's mutex without blocking. It reports whether the
// lock was taken; the unlock function is only valid when it was.
func (k *KeyedMutex) TryLock(key string) (func(), bool) {
	entry := k.reserve(key)
	if !entry.mu.TryLock() {
		k.release(key, entry)
		return nil, false
	}

	return func() {
		entry.mu.Unlock()
		k.release(key, entry)
	}, true
}
