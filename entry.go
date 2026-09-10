package concache

import "time"

type Entry[T any] struct {
	Value     T
	Error     error
	ExpiresAt time.Time

	// key is kept on the entry so that eviction can reach the map from the ordering list.
	key string
}

type EntryUpdate[T any] struct {
	Value T
	Error error

	// TTL overrides the cache's default ttl for this entry only. Zero means the default is used.
	//
	// This lets the source of the data decide how long its answer stays valid, which is useful when
	// that varies per key, for example when an upstream tells the caller how long it is caching a
	// response itself.
	TTL time.Duration
}

// EntryState describes where a returned value came from.
type EntryState int

const (
	// StateMiss means no usable value could be produced.
	StateMiss EntryState = iota
	// StateFresh means the value was just produced by the UpdateFunc.
	StateFresh
	// StateHit means a valid cached value was returned without calling the UpdateFunc.
	StateHit
	// StateStale means the cached value had expired and the UpdateFunc failed, so the expired value
	// was returned instead. The value is usable; the returned error explains why it is stale.
	StateStale
)

func (s EntryState) String() string {
	switch s {
	case StateMiss:
		return "miss"
	case StateFresh:
		return "fresh"
	case StateHit:
		return "hit"
	case StateStale:
		return "stale"
	}

	return "unknown"
}
