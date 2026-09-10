package concache

import "time"

// putEntry inserts an entry directly, the way a completed update would have left it. It exists so
// tests can set up cache state without going through an UpdateFunc.
func putEntry[T any](cache *UpdateCache[T], key string, value T, expiresAt time.Time) {

	cache.entriesLock.Lock()
	defer cache.entriesLock.Unlock()

	cache.entries[key] = cache.order.PushFront(&Entry[T]{Value: value, ExpiresAt: expiresAt, key: key})
}

func entryOf[T any](cache *UpdateCache[T], key string) *Entry[T] {

	cache.entriesLock.RLock()
	defer cache.entriesLock.RUnlock()

	element, exists := cache.entries[key]
	if !exists {
		return nil
	}

	return element.Value.(*Entry[T])
}

// expireEntry backdates an entry's expiry relative to the cache's own clock, so it works with an
// injected clock too.
func expireEntry[T any](cache *UpdateCache[T], key string) {
	entryOf(cache, key).ExpiresAt = cache.now().Add(-time.Hour)
}

func hasEntry[T any](cache *UpdateCache[T], key string) bool {
	return entryOf(cache, key) != nil
}
