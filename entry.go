package concache

import "time"

type Entry[T any] struct {
	Value     T
	Error     error
	ExpiresAt time.Time
}

type EntryUpdate[T any] struct {
	Value T
	Error error
}
