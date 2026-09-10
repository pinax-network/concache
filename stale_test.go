package concache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSourceDown = errors.New("source is unreachable")

// staleCache returns a cache whose UpdateFunc fails whenever failing is true, with a clock the test
// controls.
func staleCache(t *testing.T, maxStaleAge time.Duration) (cache *UpdateCache[string], clock *time.Time, failing *bool) {

	t.Helper()

	now := time.Now()
	shouldFail := false
	calls := 0

	options := []Option{}
	if maxStaleAge > 0 {
		options = append(options, WithMaxStaleAge(maxStaleAge))
	}

	cache = NewUpdateCache(time.Minute, func(_ context.Context, _ string) (EntryUpdate[string], error) {
		calls++
		if shouldFail {
			return EntryUpdate[string]{}, errSourceDown
		}
		return EntryUpdate[string]{Value: "value-" + time.Duration(calls).String()}, nil
	}, options...)

	cache.now = func() time.Time { return now }

	return cache, &now, &shouldFail
}

func TestGet_ReportsWhereTheValueCameFrom(t *testing.T) {

	cache, _, _ := staleCache(t, 0)

	_, state, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, StateFresh, state)

	_, state, err = cache.Get(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, StateHit, state)
}

func TestGet_ServesStaleWhileTheSourceIsDown(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	fresh, state, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)
	require.Equal(t, StateFresh, state)

	*clock = clock.Add(2 * time.Minute) // past the ttl, inside the stale window
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assert.Equal(t, StateStale, state)
	assert.Equal(t, fresh, value, "the previous value must be served rather than a zero value")
	assert.ErrorIs(t, err, errSourceDown, "the error must still explain why the value is stale")
}

func TestGet_StopsServingStalePastTheWindow(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	_, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	// ttl plus the stale window have both elapsed.
	*clock = clock.Add(11*time.Minute + time.Second)
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assert.Equal(t, StateMiss, state)
	assert.Empty(t, value)
	assert.ErrorIs(t, err, errSourceDown)
}

func TestGet_NoStaleWithoutTheOption(t *testing.T) {

	cache, clock, failing := staleCache(t, 0)

	_, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	*clock = clock.Add(2 * time.Minute)
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assert.Equal(t, StateMiss, state, "stale reads must be opt in")
	assert.Empty(t, value)
	assert.ErrorIs(t, err, errSourceDown)
}

func TestGet_RefreshesOnceTheSourceRecovers(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	first, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	*clock = clock.Add(2 * time.Minute)
	*failing = true
	_, state, _ := cache.Get(context.Background(), "key")
	require.Equal(t, StateStale, state)

	*failing = false
	value, state, err := cache.Get(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, StateFresh, state)
	assert.NotEqual(t, first, value, "a recovered source must replace the stale value")
}

func TestPrune_KeepsEntriesInsideTheStaleWindow(t *testing.T) {

	cache, clock, _ := staleCache(t, 10*time.Minute)

	_, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	*clock = clock.Add(2 * time.Minute) // expired, still stale-usable
	cache.Prune()
	assert.Equal(t, 1, cache.Len(), "an entry that can still be served stale must survive a prune")

	*clock = clock.Add(10 * time.Minute)
	cache.Prune()
	assert.Equal(t, 0, cache.Len())
}

func TestWithMaxStaleAge_NegativeIsTreatedAsZero(t *testing.T) {

	cache := NewUpdateCache(time.Minute, func(_ context.Context, _ string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: "value"}, nil
	}, WithMaxStaleAge(-time.Hour))

	_, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	cache.Prune()
	assert.Equal(t, 1, cache.Len(), "a negative stale age must never prune a fresh entry")

	expireEntry(cache, "key")
	cache.Prune()
	assert.Equal(t, 0, cache.Len(), "stale reads stay disabled, so an expired entry is pruned right away")
}
