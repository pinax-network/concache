# Update Cache

This Go library provides an in-memory cache optimized for highly concurrent access. It provides a `Get(key)`method to
look up a key-value pair within the cache. In case of cache misses it uses a pre-defined `UpdateFunc` to update the
cache entry. Features:

* Thread-safe `Get()` method to access key value pairs
* On concurrent calls to `Get()` for an uncached key, the `UpdateFunc` will only be called for the first caller. All
  subsequent calls will block and then receive the cached result.
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
			valid, hit, _ := cache.Get(context.Background(), "my_secret_key")
			fmt.Printf("requested key from cache, valid: %t, hit: %t \n", valid, hit)
			wg.Done()
		}()
	}
	wg.Wait()
}
```

Running this will result in all 3 goroutines getting the requested key, but only the first call will trigger a cache
update:

```bash
$ go run example.go
updated api key
requested key from cache, valid: true, hit: false 
requested key from cache, valid: true, hit: true 
requested key from cache, valid: true, hit: true
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

	_, hit, err := cache.Get(context.Background(), "someone")
	fmt.Printf("hit: %t, err: %s \n", hit, err)

	_, hit, err = cache.Get(context.Background(), "someone")
	fmt.Printf("hit: %t, err: %s \n", hit, err)
}
```

The second call to Get will now return the cached error instead of executing the `UpdateFunc` again:

```bash
$ go run example.go
updated user
hit: false, err: user id "someone" not found on database 
hit: true, err: user id "someone" not found on database 
```

To **not** cache any errors (because of temporary issues such as connection losses to a database), just return the error
from the `UpdateFunc`, in that case it will not be cached and every new call to `Get` will trigger the `UpdateFunc` 
again:

```golang
updateUserFunc := func (ctx context.Context, userId string) (concache.EntryUpdate[string], error) {
    return concache.EntryUpdate[string]{}, errors.New("temporary connection issues")
}
```
