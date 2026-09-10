package concache

import "time"

type config struct {
	maxEntries  int
	maxStaleAge time.Duration
}

type Option func(*config)

// WithMaxEntries bounds how many entries the cache holds. Once the limit is reached, storing a new
// entry drops the least recently stored one. Zero, the default, leaves the cache unbounded.
//
// Use this whenever the key space is driven by untrusted or open-ended input, where an unbounded
// cache would grow until the process runs out of memory.
func WithMaxEntries(maxEntries int) Option {
	return func(c *config) { c.maxEntries = maxEntries }
}

// WithMaxStaleAge lets GetEntry and GetEntryWith return an expired value when the UpdateFunc fails,
// for up to maxStaleAge past that entry's expiry. Zero, the default, disables stale reads.
//
// This trades freshness for availability: a caller that would rather serve the previous answer than
// fail can keep working while its data source is unreachable. Get never returns a stale value,
// whatever this is set to, so enabling it cannot change the behaviour of existing callers.
func WithMaxStaleAge(maxStaleAge time.Duration) Option {
	return func(c *config) { c.maxStaleAge = maxStaleAge }
}
