package concache

import (
	"context"
	"errors"
	"testing"
	"time"
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
	requireNoError(t, err)
	assertEqual(t, state, StateFresh)

	_, state, err = cache.Get(context.Background(), "key")
	requireNoError(t, err)
	assertEqual(t, state, StateHit)
}

func TestGet_ServesStaleWhileTheSourceIsDown(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	fresh, state, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)
	requireEqual(t, state, StateFresh)

	*clock = clock.Add(2 * time.Minute) // past the ttl, inside the stale window
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assertEqual(t, state, StateStale)
	assertEqual(t, value, fresh, "the previous value must be served rather than a zero value")
	assertErrorIs(t, err, errSourceDown, "the error must still explain why the value is stale")
}

func TestGet_StopsServingStalePastTheWindow(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	_, _, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)

	// ttl plus the stale window have both elapsed.
	*clock = clock.Add(11*time.Minute + time.Second)
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assertEqual(t, state, StateMiss)
	assertEqual(t, value, "")
	assertErrorIs(t, err, errSourceDown)
}

func TestGet_NoStaleWithoutTheOption(t *testing.T) {

	cache, clock, failing := staleCache(t, 0)

	_, _, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)

	*clock = clock.Add(2 * time.Minute)
	*failing = true

	value, state, err := cache.Get(context.Background(), "key")

	assertEqual(t, state, StateMiss, "stale reads must be opt in")
	assertEqual(t, value, "")
	assertErrorIs(t, err, errSourceDown)
}

func TestGet_RefreshesOnceTheSourceRecovers(t *testing.T) {

	cache, clock, failing := staleCache(t, 10*time.Minute)

	first, _, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)

	*clock = clock.Add(2 * time.Minute)
	*failing = true
	_, state, _ := cache.Get(context.Background(), "key")
	requireEqual(t, state, StateStale)

	*failing = false
	value, state, err := cache.Get(context.Background(), "key")

	requireNoError(t, err)
	assertEqual(t, state, StateFresh)
	assertNotEqual(t, value, first, "a recovered source must replace the stale value")
}

func TestPrune_KeepsEntriesInsideTheStaleWindow(t *testing.T) {

	cache, clock, _ := staleCache(t, 10*time.Minute)

	_, _, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)

	*clock = clock.Add(2 * time.Minute) // expired, still stale-usable
	cache.Prune()
	assertEqual(t, cache.Len(), 1, "an entry that can still be served stale must survive a prune")

	*clock = clock.Add(10 * time.Minute)
	cache.Prune()
	assertEqual(t, cache.Len(), 0)
}

func TestWithMaxStaleAge_NegativeIsTreatedAsZero(t *testing.T) {

	cache := NewUpdateCache(time.Minute, func(_ context.Context, _ string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: "value"}, nil
	}, WithMaxStaleAge(-time.Hour))

	_, _, err := cache.Get(context.Background(), "key")
	requireNoError(t, err)

	cache.Prune()
	assertEqual(t, cache.Len(), 1, "a negative stale age must never prune a fresh entry")

	expireEntry(cache, "key")
	cache.Prune()
	assertEqual(t, cache.Len(), 0, "stale reads stay disabled, so an expired entry is pruned right away")
}
