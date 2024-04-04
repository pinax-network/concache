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
