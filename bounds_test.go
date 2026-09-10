package concache

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func echoCache(t *testing.T, options ...Option) *UpdateCache[string] {

	t.Helper()

	return NewUpdateCache(time.Minute, func(_ context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: key}, nil
	}, options...)
}

func TestWithMaxEntries_EvictsLeastRecentlyStored(t *testing.T) {

	cache := echoCache(t, WithMaxEntries(2))

	for _, key := range []string{"a", "b", "c"} {
		_, _, err := cache.Get(context.Background(), key)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, cache.Len())
	assert.False(t, hasEntry(cache, "a"), "the least recently stored entry must be evicted")
	assert.True(t, hasEntry(cache, "b"))
	assert.True(t, hasEntry(cache, "c"))
}

func TestWithMaxEntries_RewriteRefreshesPosition(t *testing.T) {

	cache := echoCache(t, WithMaxEntries(2))
	cache.now = func() time.Time { return time.Now() }

	putEntry(cache, "a", "a", time.Now().Add(-time.Hour)) // expired, so the next Get rewrites it
	putEntry(cache, "b", "b", time.Now().Add(time.Hour))

	_, _, err := cache.Get(context.Background(), "a")
	require.NoError(t, err)

	_, _, err = cache.Get(context.Background(), "c")
	require.NoError(t, err)

	assert.True(t, hasEntry(cache, "a"), "the rewritten entry must not be the eviction candidate")
	assert.False(t, hasEntry(cache, "b"))
}

func TestWithMaxEntries_UnboundedByDefault(t *testing.T) {

	cache := echoCache(t)

	for i := 0; i < 200; i++ {
		_, _, err := cache.Get(context.Background(), strconv.Itoa(i))
		require.NoError(t, err)
	}

	assert.Equal(t, 200, cache.Len())
}

func TestEntryUpdate_TTLOverridesTheDefault(t *testing.T) {

	now := time.Now()
	perKeyTTL := map[string]time.Duration{"short": time.Second, "long": time.Hour}

	cache := NewUpdateCache(time.Minute, func(_ context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: key, TTL: perKeyTTL[key]}, nil
	})
	cache.now = func() time.Time { return now }

	for _, key := range []string{"short", "long", "default"} {
		_, _, err := cache.Get(context.Background(), key)
		require.NoError(t, err)
	}

	assert.Equal(t, now.Add(time.Second), entryOf(cache, "short").ExpiresAt)
	assert.Equal(t, now.Add(time.Hour), entryOf(cache, "long").ExpiresAt)
	assert.Equal(t, now.Add(time.Minute), entryOf(cache, "default").ExpiresAt, "a zero ttl must fall back to the cache default")
}

func TestGetWith_UsesThePerCallUpdateFunc(t *testing.T) {

	cache := NewUpdateCache[string](time.Minute, nil)

	value, state, err := cache.GetWith(context.Background(), "key", func(_ context.Context, _ string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: "from the caller"}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, StateFresh, state)
	assert.Equal(t, "from the caller", value)

	// The value is cached like any other, so a later read does not need an update function at all.
	value, state, err = cache.Get(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, StateHit, state)
	assert.Equal(t, "from the caller", value)
}

func TestGetWith_SingleFlightPerKey(t *testing.T) {

	cache := NewUpdateCache[string](time.Minute, nil)

	calls := 0
	callsLock := sync.Mutex{}

	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, _, err := cache.GetWith(context.Background(), "key", func(_ context.Context, _ string) (EntryUpdate[string], error) {
				callsLock.Lock()
				calls++
				callsLock.Unlock()

				time.Sleep(100 * time.Millisecond)

				return EntryUpdate[string]{Value: "value"}, nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	callsLock.Lock()
	defer callsLock.Unlock()
	assert.Equal(t, 1, calls, "concurrent readers of one key must share a single update")
}

func TestGet_WithoutAnUpdateFunc(t *testing.T) {

	cache := NewUpdateCache[string](time.Minute, nil)

	_, state, err := cache.Get(context.Background(), "key")

	assert.Equal(t, StateMiss, state)
	assert.ErrorIs(t, err, ErrNoUpdateFunc)
}

func TestWithMaxEntries_NegativeIsUnbounded(t *testing.T) {

	cache := echoCache(t, WithMaxEntries(-1))

	for i := 0; i < 200; i++ {
		_, _, err := cache.Get(context.Background(), strconv.Itoa(i))
		require.NoError(t, err)
	}

	assert.Equal(t, 200, cache.Len())
}
