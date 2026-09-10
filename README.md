# Update Cache

This Go library provides an in-memory cache optimized for highly concurrent access. It provides a `Get(key)` method to
look up a key-value pair within the cache. In case of cache misses it uses a pre-defined `UpdateFunc` to update the
cache entry. Features:

* Thread-safe `Get()` method to access key value pairs, reporting whether the value was a cache hit, freshly updated
  or served stale
* On concurrent calls to `Get()` for an uncached key, the `UpdateFunc` will only be called for the first caller. All
  subsequent calls will block and then receive the cached result. Should the `UpdateFunc` fail, the next waiting
  caller retries it.
* The `UpdateFunc` can decide whether errors should be cached or not. Either way `Get()` will return this error to the
  caller transparently.

```
go get github.com/pinax-network/concache
```

## Example Code

### Concurrent Access

Concurrent access to `Get` will be thread safe and prevent multiple calls to the `UpdateFunc`:

```golang
package main

import (
	"context"
	"fmt"
	"github.com/pinax-network/concache"
	"sync"
	"time"
)

func main() {
	// updateKeyValidFunc checks if the given api key is valid
	updateKeyValidFunc := func(ctx context.Context, apiKey string) (concache.EntryUpdate[bool], error) {
		// here you would do an expensive request, such as a database lookup to check if the given api key is valid
		// we just simulate this using a sleep
		time.Sleep(100 * time.Millisecond)
		fmt.Println("updated api key")

		return concache.EntryUpdate[bool]{
			Value: apiKey == "my_secret_key",
			Error: nil,
		}, nil
	}

	cache := concache.NewUpdateCache(5*time.Minute, updateKeyValidFunc)

	wg := sync.WaitGroup{}
	for _ = range 3 {
		wg.Add(1)
		go func() {
			valid, state, _ := cache.Get(context.Background(), "my_secret_key")
			fmt.Printf("requested key from cache, valid: %t, state: %s \n", valid, state)
			wg.Done()
		}()
	}
	wg.Wait()
}
```

Running this will result in all 3 goroutines getting the requested key, but only the first call will trigger a cache
update (as long as it succeeds; a failed update is retried by the next waiting caller):

```bash
$ go run example.go
updated api key
requested key from cache, valid: true, state: fresh 
requested key from cache, valid: true, state: hit 
requested key from cache, valid: true, state: hit
```

### Caching Errors

To cache errors and ensure the `UpdateFunc` will only be called again after the cache entry expires, the error can be
embedded into `concache.EntryUpdate`.

```golang
package main

import (
	"context"
	"fmt"
	"github.com/pinax-network/concache"
	"time"
)

func main() {
	// updateUserFunc looks up a user by the given id and returns their username
	updateUserFunc := func(ctx context.Context, userId string) (concache.EntryUpdate[string], error) {
		// here you would do an expensive request, such as a database lookup to load the user
		fmt.Println("updated user")
		if userId == "me" {
			return concache.EntryUpdate[string]{
				Value: "myself",
			}, nil
		}
		return concache.EntryUpdate[string]{
			Error: fmt.Errorf("user id %q not found on database", userId),
		}, nil
	}

	cache := concache.NewUpdateCache(5*time.Minute, updateUserFunc)

	_, state, err := cache.Get(context.Background(), "someone")
	fmt.Printf("state: %s, err: %s \n", state, err)

	_, state, err = cache.Get(context.Background(), "someone")
	fmt.Printf("state: %s, err: %s \n", state, err)
}
```

The second call to Get will now return the cached error instead of executing the `UpdateFunc` again:

```bash
$ go run example.go
updated user
state: fresh, err: user id "someone" not found on database 
state: hit, err: user id "someone" not found on database 
```

To **not** cache any errors (because of temporary issues such as connection losses to a database), just return the error
from the `UpdateFunc`, in that case it will not be cached and every new call to `Get` will trigger the `UpdateFunc` 
again:

```golang
updateUserFunc := func (ctx context.Context, userId string) (concache.EntryUpdate[string], error) {
    return concache.EntryUpdate[string]{}, errors.New("temporary connection issues")
}
```

## Bounding the cache

By default the cache grows with the key space. When keys come from open-ended or untrusted input,
bound it:

```golang
cache := concache.NewUpdateCache(5*time.Minute, updateFunc, concache.WithMaxEntries(50_000))
```

Once the limit is reached, storing a new entry drops the least recently stored one. Note that
`Prune()` still has to be called periodically to release entries that expired while the cache sat
below the limit:

```golang
go func() {
    for range time.Tick(time.Minute) {
        cache.Prune()
    }
}()
```

## Serving stale values while the source is down

`Get` returns an error whenever the entry is expired and the `UpdateFunc` fails, which means a
temporary outage of the underlying data source turns into an outage for the caller. When serving the
previous answer is better than serving none, allow stale reads and check the returned state:

```golang
cache := concache.NewUpdateCache(time.Minute, updateFunc, concache.WithMaxStaleAge(time.Hour))

value, state, err := cache.Get(ctx, "my_key")
switch state {
case concache.StateHit, concache.StateFresh:
    // value is current
case concache.StateStale:
    // value is the previous answer, err explains why it could not be refreshed
case concache.StateMiss:
    // nothing usable, err explains why
}
```

An entry may be served stale until `maxStaleAge` past its expiry, after which it is dropped. That
bound matters: it decides how long the cache keeps handing out an answer that may no longer be true.

A stale value always comes with the error that prevented the refresh, so callers that treat any
error as fatal keep failing exactly as they would without the option.

## Per-entry TTL

An `UpdateFunc` can override the cache's ttl for the entry it just produced by setting
`EntryUpdate.TTL`. This is useful when the data source tells you how long its answer is good for:

```golang
return concache.EntryUpdate[Response]{Value: response, TTL: response.CacheFor}, nil
```

A zero `TTL` uses the cache default.

## Supplying the update function per call

`GetWith` takes the `UpdateFunc` as an argument, for callers whose key cannot carry everything
the update needs — for example when the key is a hash of a request rather than the request itself:

```golang
cache := concache.NewUpdateCache[Response](time.Minute, nil, concache.WithMaxStaleAge(time.Hour))

value, state, err := cache.GetWith(ctx, hashOf(request), func(ctx context.Context, _ string) (concache.EntryUpdate[Response], error) {
    return load(ctx, request)
})
```

Concurrent calls on the same key are still serialized. A successful update is shared with all
waiting callers, so only the first update function runs. A failed update is not cached, so the next
waiting caller runs its own update function instead.
