package concache

import (
	"context"
	"sync"
	"time"
)

// UpdateFunc updates the given key from its underlying data source. It returns an EntryUpdate or an error.
//
// Note the error handling here:
//   - In case the UpdateFunc returns an error, then the EntryUpdate won't be stored within the cache and the UpdateFunc
//     will be triggered again on the next Get call. This should be used for transient errors (for example, when the
//     UpdateFunc was unable to connect to the underlying database because of a network issue).
//   - In case the error is embedded in EntryUpdate.Error, it will be cached and UpdateFunc is only going to be triggered
//     again in case the Entry expires. This is useful for persistent errors and avoids spamming unnecessary requests
//     to the underlying database.
//
// Either error will be returned whenever Get is being called, so the error handling is consistent on the client side.
type UpdateFunc[T any] func(ctx context.Context, key string) (EntryUpdate[T], error)

// UpdateCache is an in-memory cache that provides only a Get method to retrieve values from a given key. In case the
// entry is missing or expired, it will be updated within the Get method from the UpdateFunc.
type UpdateCache[T any] struct {
	keyLocks    *keyedMutex
	entriesLock *sync.Mutex
	ttl         time.Duration
	entries     map[string]*Entry[T]
	updateFunc  UpdateFunc[T]
}

// NewUpdateCache returns a new UpdateCache that sets the entry's expiration based on the given ttl. Whenever an entry
// is not available or expired, it will be updated using the given UpdateFunc.
//
// The UpdateCache is thread-safe and can be called from multiple goroutines.
func NewUpdateCache[T any](ttl time.Duration, updateFunc UpdateFunc[T]) *UpdateCache[T] {
	return &UpdateCache[T]{
		keyLocks:    newKeyedMutex(),
		entriesLock: &sync.Mutex{},
		ttl:         ttl,
		entries:     make(map[string]*Entry[T]),
		updateFunc:  updateFunc,
	}
}

// Get returns the entry for the given key. In case the entry is cached and not expired, it will return it immediately
// from the cache. Otherwise, Get will try to update the entry using the UpdateFunc.
//
// This method is thread-safe and can be called from multiple goroutines. If there are multiple concurrent calls on the
// same uncached key, the UpdateFunc will only be executed on the first call while all subsequent calls will block until
// the cache has been updated.
func (c *UpdateCache[T]) Get(ctx context.Context, key string) (res T, hit bool, err error) {

	unlock := c.keyLocks.Lock(key)
	defer unlock()

	if entry, exists := c.entries[key]; exists && entry.ExpiresAt.After(time.Now()) {
		return entry.Value, true, entry.Error
	} else {
		entry, err := c.updateFunc(ctx, key)

		// In case we receive an *UpdateFunc* error here, we won't store the result and just return the error here.
		// In this case, we want to retry loading the entry again on the next Get call.
		if err != nil {
			return res, false, err
		}

		// acquire a write lock on the cache to update the entry
		c.entriesLock.Lock()
		defer c.entriesLock.Unlock()

		// In case the error is embedded within the EntryUpdate, we still return it as the error below, but also update
		// the cache. This allows us to cache persistent errors and reduce the load on any underlying datasource by not
		// calling the UpdateFunc again until the cache entry expires.
		c.entries[key] = &Entry[T]{
			Value:     entry.Value,
			Error:     entry.Error,
			ExpiresAt: time.Now().Add(c.ttl),
		}

		return entry.Value, false, entry.Error
	}
}

// Prune removes all expired entries from the cache.
func (c *UpdateCache[T]) Prune() {

	c.entriesLock.Lock()
	defer c.entriesLock.Unlock()

	for key, value := range c.entries {
		if value.ExpiresAt.Before(time.Now()) {
			delete(c.entries, key) // delete the cache entry
			c.keyLocks.Delete(key) // delete the key lock
		}
	}
}
