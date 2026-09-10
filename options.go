package concache

import "time"

type config struct {
	maxEntries  int
	maxStaleAge time.Duration
}

type Option func(*config)

// WithMaxEntries bounds how many entries the cache holds. Once the limit is reached, storing a new
// entry drops the least recently stored one. Zero, the default, leaves the cache unbounded, and so
// does any negative value.
//
// Use this whenever the key space is driven by untrusted or open-ended input, where an unbounded
// cache would grow until the process runs out of memory.
func WithMaxEntries(maxEntries int) Option {
	return func(c *config) { c.maxEntries = max(maxEntries, 0) }
}

// WithMaxStaleAge lets Get and GetWith return an expired value as StateStale when the UpdateFunc
// fails, for up to maxStaleAge past that entry's expiry. Zero, the default, disables stale reads, and
// so does any negative value; a negative value must never shorten the lifetime of a fresh entry.
//
// This trades freshness for availability: a caller that would rather serve the previous answer than
// fail can keep working while its data source is unreachable. A stale value is always accompanied
// by the error that prevented the refresh, so callers that treat any error as fatal are unaffected.
func WithMaxStaleAge(maxStaleAge time.Duration) Option {
	return func(c *config) { c.maxStaleAge = max(maxStaleAge, 0) }
}
