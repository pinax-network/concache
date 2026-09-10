package concache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNoUpdateFunc is returned when a lookup would have to call an UpdateFunc but neither the cache
// nor the call supplied one.
var ErrNoUpdateFunc = errors.New("concache: no update function configured")

// UpdateFunc updates the given key from its underlying data source. It returns an EntryUpdate or an
// error.
//
// Note the error handling here:
//   - In case the UpdateFunc returns an error, then the EntryUpdate won't be stored within the cache
//     and the UpdateFunc will be triggered again on the next Get call. This should be used for
//     transient errors (for example, when the UpdateFunc was unable to connect to the underlying
//     database because of a network issue).
//   - In case the error is embedded in EntryUpdate.Error, it will be cached and UpdateFunc is only
//     going to be triggered again in case the Entry expires. This is useful for persistent errors
//     and avoids spamming unnecessary requests to the underlying database.
//
// Either error will be returned whenever Get is being called, so the error handling is consistent on
// the client side.
type UpdateFunc[T any] func(ctx context.Context, key string) (EntryUpdate[T], error)

// UpdateCache is an in-memory cache that provides only a Get method to retrieve values from a given
// key. In case the entry is missing or expired, it will be updated within the Get method from the
// UpdateFunc.
type UpdateCache[T any] struct {
	keyLocks    *keyedMutex
	entriesLock sync.RWMutex
	entries     map[string]*list.Element
	order       *list.List

	ttl         time.Duration
	maxEntries  int
	maxStaleAge time.Duration
	updateFunc  UpdateFunc[T]
	now         func() time.Time
}

// NewUpdateCache returns a new UpdateCache that sets the entry's expiration based on the given ttl.
// Whenever an entry is not available or expired, it will be updated using the given UpdateFunc. An
// individual update may override the ttl through EntryUpdate.TTL.
//
// See WithMaxEntries and WithMaxStaleAge for the available options. The UpdateFunc may be nil for a
// cache that is only used through GetEntryWith.
//
// The UpdateCache is thread-safe and can be called from multiple goroutines.
func NewUpdateCache[T any](ttl time.Duration, updateFunc UpdateFunc[T], options ...Option) *UpdateCache[T] {

	cfg := config{}
	for _, option := range options {
		option(&cfg)
	}

	return &UpdateCache[T]{
		keyLocks:    newKeyedMutex(),
		entries:     make(map[string]*list.Element),
		order:       list.New(),
		ttl:         ttl,
		maxEntries:  cfg.maxEntries,
		maxStaleAge: cfg.maxStaleAge,
		updateFunc:  updateFunc,
		now:         time.Now,
	}
}

// Get returns the entry for the given key. In case the entry is cached and not expired, it will
// return it immediately from the cache. Otherwise, Get will try to update the entry using the
// UpdateFunc.
//
// Get never returns a stale value, regardless of WithMaxStaleAge. Use GetEntry for that.
//
// This method is thread-safe and can be called from multiple goroutines. If there are multiple
// concurrent calls on the same uncached key, the UpdateFunc will only be executed on the first call
// while all subsequent calls will block until the cache has been updated.
func (c *UpdateCache[T]) Get(ctx context.Context, key string) (res T, hit bool, err error) {

	value, state, err := c.get(ctx, key, c.updateFunc, false)

	return value, state == StateHit, err
}

// GetEntry behaves like Get but also reports where the value came from, and may return an expired
// value when the cache was built WithMaxStaleAge and the UpdateFunc failed.
//
// When the returned state is StateStale the value is usable and the error explains why it could not
// be refreshed. Callers that treat any non-nil error as fatal should use Get instead.
func (c *UpdateCache[T]) GetEntry(ctx context.Context, key string) (res T, state EntryState, err error) {
	return c.get(ctx, key, c.updateFunc, true)
}

// GetEntryWith behaves like GetEntry but takes the UpdateFunc per call.
//
// This is for callers whose key cannot carry everything the update needs, for example when the key
// is a hash of the request rather than the request itself. Single-flight still applies per key, so
// on concurrent calls only the first updateFunc runs.
func (c *UpdateCache[T]) GetEntryWith(ctx context.Context, key string, updateFunc UpdateFunc[T]) (res T, state EntryState, err error) {
	return c.get(ctx, key, updateFunc, true)
}

func (c *UpdateCache[T]) get(ctx context.Context, key string, updateFunc UpdateFunc[T], allowStale bool) (T, EntryState, error) {

	var zero T

	unlock := c.keyLocks.Lock(key)
	defer unlock()

	cached, cachedExists := c.lookup(key)
	if cachedExists && cached.ExpiresAt.After(c.now()) {
		return cached.Value, StateHit, cached.Error
	}

	if updateFunc == nil {
		return zero, StateMiss, ErrNoUpdateFunc
	}

	update, err := updateFunc(ctx, key)

	// In case we receive an *UpdateFunc* error here, we won't store the result and just return the
	// error. In this case, we want to retry loading the entry again on the next Get call.
	if err != nil {
		// Serving the expired value beats serving nothing, for callers that asked for it and for as
		// long as the entry has not gone past its stale window.
		if allowStale && cachedExists && c.isWithinStaleWindow(cached) {
			return cached.Value, StateStale, err
		}

		return zero, StateMiss, err
	}

	// In case the error is embedded within the EntryUpdate, we still return it as the error below,
	// but also update the cache. This allows us to cache persistent errors and reduce the load on
	// any underlying datasource by not calling the UpdateFunc again until the cache entry expires.
	c.store(key, update)

	return update.Value, StateFresh, update.Error
}

// Prune removes all entries that can no longer be served, including as a stale value.
func (c *UpdateCache[T]) Prune() {

	c.entriesLock.Lock()
	defer c.entriesLock.Unlock()

	for _, element := range c.entries {
		if !c.isWithinStaleWindow(element.Value.(*Entry[T])) {
			c.removeLocked(element)
		}
	}
}

// Len returns the number of entries currently held.
func (c *UpdateCache[T]) Len() int {

	c.entriesLock.RLock()
	defer c.entriesLock.RUnlock()

	return c.order.Len()
}

func (c *UpdateCache[T]) lookup(key string) (*Entry[T], bool) {

	c.entriesLock.RLock()
	defer c.entriesLock.RUnlock()

	element, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	return element.Value.(*Entry[T]), true
}

func (c *UpdateCache[T]) store(key string, update EntryUpdate[T]) {

	ttl := c.ttl
	if update.TTL > 0 {
		ttl = update.TTL
	}

	entry := &Entry[T]{
		Value:     update.Value,
		Error:     update.Error,
		ExpiresAt: c.now().Add(ttl),
		key:       key,
	}

	c.entriesLock.Lock()
	defer c.entriesLock.Unlock()

	if element, exists := c.entries[key]; exists {
		element.Value = entry
		c.order.MoveToFront(element)
	} else {
		c.entries[key] = c.order.PushFront(entry)
	}

	// Entries are ordered by write time, so the back of the list is the least recently stored one.
	for c.maxEntries > 0 && c.order.Len() > c.maxEntries {
		c.removeLocked(c.order.Back())
	}
}

// isWithinStaleWindow reports whether an expired entry may still be served. With no stale age
// configured this is simply "not expired".
func (c *UpdateCache[T]) isWithinStaleWindow(entry *Entry[T]) bool {
	return !c.now().After(entry.ExpiresAt.Add(c.maxStaleAge))
}

func (c *UpdateCache[T]) removeLocked(element *list.Element) {

	if element == nil {
		return
	}

	delete(c.entries, element.Value.(*Entry[T]).key)
	c.order.Remove(element)
}
