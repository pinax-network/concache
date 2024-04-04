package concache

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
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
	res, hit, err := testCache.Get(context.Background(), "test_key")
	assert.NoError(t, err)
	assert.Equal(t, "test_result", res)
	assert.Equal(t, false, hit)
	assert.Equal(t, 1, updateCnt)

	// test the same key to ensure it's cached and UpdateFunc isn't called again
	res, hit, err = testCache.Get(context.Background(), "test_key")
	assert.NoError(t, err)
	assert.Equal(t, "test_result", res)
	assert.Equal(t, true, hit)
	assert.Equal(t, 1, updateCnt)
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
			assert.NoError(t, err)
			assert.Equal(t, "test_result", res)
			wg.Done()
		}()
	}

	wg.Wait()
	assert.Equal(t, 1, updateCnt)
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
	testCache.entries["test_key"] = &Entry[string]{
		Value:     "test_result",
		Error:     nil,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// test we get a cached response
	res, hit, err := testCache.Get(context.Background(), "test_key")
	assert.NoError(t, err)
	assert.Equal(t, "test_result", res)
	assert.Equal(t, true, hit)
	assert.Equal(t, 0, updateCnt)

	// test we update the expired entry
	testCache.entries["test_key"].ExpiresAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	res, hit, err = testCache.Get(context.Background(), "test_key")
	assert.NoError(t, err)
	assert.Equal(t, "test_result", res)
	assert.Equal(t, false, hit)
	assert.Equal(t, 1, updateCnt)
}

func TestCache_GetError(t *testing.T) {

	updateCnt := 0
	testError := errors.New("test_error")

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{}, testError
	})

	// we should get a test error
	_, hit, err := testCache.Get(context.Background(), "test_key")
	assert.Equal(t, testError, err)
	assert.Equal(t, 1, updateCnt)
	assert.Equal(t, false, hit)

	// no entry should be cached, so calling Get() again should trigger the UpdateFunc
	_, hit, err = testCache.Get(context.Background(), "test_key")
	assert.Equal(t, testError, err)
	assert.Equal(t, 2, updateCnt)
	assert.Equal(t, false, hit)
}

func TestCache_GetEmbeddedError(t *testing.T) {

	updateCnt := 0
	testError := errors.New("test_error")

	testCache := NewUpdateCache(5*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		updateCnt++
		return EntryUpdate[string]{Value: "", Error: testError}, nil
	})

	// we should get a test error
	_, hit, err := testCache.Get(context.Background(), "test_key")
	assert.Equal(t, testError, err)
	assert.Equal(t, 1, updateCnt)
	assert.Equal(t, false, hit)

	// as the error is embedded, it should be cached
	_, hit, err = testCache.Get(context.Background(), "test_key")
	assert.Equal(t, testError, err)
	assert.Equal(t, 1, updateCnt)
	assert.Equal(t, true, hit)
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
	assert.Equal(t, 3, updateCnt)
}

func TestCache_Prune(t *testing.T) {

	notImplementedError := errors.New("not implemented")

	testCache := NewUpdateCache(1*time.Minute, func(ctx context.Context, key string) (EntryUpdate[string], error) {
		return EntryUpdate[string]{}, notImplementedError
	})
	testCache.entries["test_key"] = &Entry[string]{
		Value:     "test_result",
		Error:     nil,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	testCache.entries["test_key_expired"] = &Entry[string]{
		Value:     "test_result_expired",
		Error:     nil,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// we load it first to initialize the locks
	res, hit, err := testCache.Get(context.Background(), "test_key_expired")
	assert.Equal(t, true, hit)
	assert.NoError(t, err)
	assert.Equal(t, "test_result_expired", res)

	_, hasValue := testCache.entries["test_key_expired"]
	assert.Equal(t, true, hasValue)
	_, hasLock := testCache.keyLocks.locks["test_key_expired"]
	assert.Equal(t, true, hasLock)

	// now we set it expired
	testCache.entries["test_key_expired"].ExpiresAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	// prune the cache to remove test_key_expired
	testCache.Prune()

	// the expired entry should be gone now
	_, hasValue = testCache.entries["test_key_expired"]
	assert.Equal(t, false, hasValue)
	_, hasLock = testCache.keyLocks.locks["test_key_expired"]
	assert.Equal(t, false, hasLock)
	_, hit, err = testCache.Get(context.Background(), "test_key_expired")
	assert.Equal(t, false, hit)
	assert.Equal(t, notImplementedError, err)

	// the test_key should be still available
	res, hit, err = testCache.Get(context.Background(), "test_key")
	assert.Equal(t, true, hit)
	assert.NoError(t, err)
	assert.Equal(t, "test_result", res)

	_, hasValue = testCache.entries["test_key"]
	assert.Equal(t, true, hasValue)
	_, hasLock = testCache.keyLocks.locks["test_key"]
	assert.Equal(t, true, hasLock)
}
