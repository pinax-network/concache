package concache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestCache_Get(t *testing.T) {

	updateCnt := 0

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{
			Value: "test_result",
			Error: nil,
		}, nil
	})

	// test we get a valid response
	res, state, err := testCache.Get(context.Background(), "test_key")
	assertNoError(t, err)
	assertEqual(t, res, "test_result")
	assertEqual(t, state, StateFresh)
	assertEqual(t, updateCnt, 1)

	// test the same key to ensure it's cached and UpdateFunc isn't called again
	res, state, err = testCache.Get(context.Background(), "test_key")
	assertNoError(t, err)
	assertEqual(t, res, "test_result")
	assertEqual(t, state, StateHit)
	assertEqual(t, updateCnt, 1)
}

func TestCache_GetParallel(t *testing.T) {

	updateCnt := 0

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		time.Sleep(1 * time.Second)
		return EntryUpdate[string]{
			Value: "test_result",
			Error: nil,
		}, nil
	})

	// run 10 requests in goroutines, all should get a valid response, cache should only be updated once
	wg := &sync.WaitGroup{}
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			res, _, err := testCache.Get(context.Background(), "test_key")
			assertNoError(t, err)
			assertEqual(t, res, "test_result")
			wg.Done()
		}()
	}

	wg.Wait()
	assertEqual(t, updateCnt, 1)
}

func TestCache_GetParallelDistinctKeys(t *testing.T) {

	// Each key has its own keyed mutex, so concurrent Get calls on different keys read and write the shared entries
	// map at the same time. This reproduces the "concurrent map read and map write" fatal error when run with -race.
	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: key}, nil
	})

	wg := &sync.WaitGroup{}
	for i := 0; i < 100; i++ {
		key := strconv.Itoa(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				res, _, err := testCache.Get(context.Background(), key)
				assertNoError(t, err)
				assertEqual(t, res, key)
			}
		}()
	}

	wg.Wait()
}

func TestCache_GetExpires(t *testing.T) {

	updateCnt := 0

	testCache := NewUpdateCache(1*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{
			Value: "test_result",
			Error: nil,
		}, nil
	})
	putEntry(testCache, "test_key", "test_result", time.Now().Add(5*time.Minute))

	// test we get a cached response
	res, state, err := testCache.Get(context.Background(), "test_key")
	assertNoError(t, err)
	assertEqual(t, res, "test_result")
	assertEqual(t, state, StateHit)
	assertEqual(t, updateCnt, 0)

	// test we update the expired entry
	expireEntry(testCache, "test_key")
	res, state, err = testCache.Get(context.Background(), "test_key")
	assertNoError(t, err)
	assertEqual(t, res, "test_result")
	assertEqual(t, state, StateFresh)
	assertEqual(t, updateCnt, 1)
}

func TestCache_GetError(t *testing.T) {

	updateCnt := 0
	testError := errors.New("test_error")

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{}, testError
	})

	// we should get a test error
	_, state, err := testCache.Get(context.Background(), "test_key")
	assertErrorIs(t, err, testError)
	assertEqual(t, updateCnt, 1)
	assertEqual(t, state, StateMiss)

	// no entry should be cached, so calling Get() again should trigger the UpdateFunc
	_, state, err = testCache.Get(context.Background(), "test_key")
	assertErrorIs(t, err, testError)
	assertEqual(t, updateCnt, 2)
	assertEqual(t, state, StateMiss)
}

func TestCache_GetEmbeddedError(t *testing.T) {

	updateCnt := 0
	testError := errors.New("test_error")

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{Value: "", Error: testError}, nil
	})

	// we should get a test error
	_, state, err := testCache.Get(context.Background(), "test_key")
	assertErrorIs(t, err, testError)
	assertEqual(t, updateCnt, 1)
	assertEqual(t, state, StateFresh)

	// as the error is embedded, it should be cached
	_, state, err = testCache.Get(context.Background(), "test_key")
	assertErrorIs(t, err, testError)
	assertEqual(t, updateCnt, 1)
	assertEqual(t, state, StateHit)
}

func TestCache_GetParallelErrors(t *testing.T) {

	updateCnt := 0
	testError := errors.New("test_error")

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		time.Sleep(1 * time.Second)

		// we error the first 3 calls and afterwards return a valid response
		if updateCnt <= 2 {
			return EntryUpdate[string]{}, testError
		} else {
			return EntryUpdate[string]{
				Value: "test_result",
				Error: nil,
			}, nil
		}
	})

	// run 10 requests in goroutines, all should get a valid response, cache should only be updated once
	wg := &sync.WaitGroup{}
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _, _ = testCache.Get(context.Background(), "test_key")
			wg.Done()
		}()
	}

	wg.Wait()
	assertEqual(t, updateCnt, 3)
}

func TestCache_Prune(t *testing.T) {

	notImplementedError := errors.New("not implemented")

	testCache := NewUpdateCache(1*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{}, notImplementedError
	})
	putEntry(testCache, "test_key", "test_result", time.Now().Add(5*time.Minute))
	putEntry(testCache, "test_key_expired", "test_result_expired", time.Now().Add(5*time.Minute))

	// we load it first to initialize the locks
	res, state, err := testCache.Get(context.Background(), "test_key_expired")
	assertEqual(t, state, StateHit)
	assertNoError(t, err)
	assertEqual(t, res, "test_result_expired")
	assertTrue(t, hasEntry(testCache, "test_key_expired"))

	// now we set it expired
	expireEntry(testCache, "test_key_expired")

	// prune the cache to remove test_key_expired
	testCache.Prune()

	// the expired entry should be gone now
	assertFalse(t, hasEntry(testCache, "test_key_expired"))
	_, state, err = testCache.Get(context.Background(), "test_key_expired")
	assertEqual(t, state, StateMiss)
	assertErrorIs(t, err, notImplementedError)

	// the test_key should be still available
	res, state, err = testCache.Get(context.Background(), "test_key")
	assertEqual(t, state, StateHit)
	assertNoError(t, err)
	assertEqual(t, res, "test_result")
	assertTrue(t, hasEntry(testCache, "test_key"))
}

func TestCache_KeyLocksDoNotLeak(t *testing.T) {

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{Value: key}, nil
	})

	for i := 0; i < 100; i++ {
		_, _, err := testCache.Get(context.Background(), strconv.Itoa(i))
		assertNoError(t, err)
	}

	// The locks are reference counted, so they are gone once released. Otherwise the lock map would
	// grow with the key space even when the entries themselves are bounded.
	testCache.keyLocks.mapLock.Lock()
	defer testCache.keyLocks.mapLock.Unlock()
	assertEqual(t, len(testCache.keyLocks.locks), 0, "released key locks must not be retained")
}
