package concache

import (
	"sync"
)

// keyedMutex hands out one mutex per key.
//
// The mutexes are reference counted and removed once the last holder releases them. That keeps the
// map from growing with the key space, and it means a mutex can never be removed while somebody
// still holds it, which would otherwise let a second caller create a fresh mutex for the same key
// and run concurrently with the first.
type keyedMutex struct {
	mapLock sync.Mutex
	locks   map[string]*countedLock
}

type countedLock struct {
	mutex   sync.Mutex
	holders int
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[string]*countedLock)}
}

func (m *keyedMutex) Lock(key string) func() {

	m.mapLock.Lock()
	lock, found := m.locks[key]
	if !found {
		lock = &countedLock{}
		m.locks[key] = lock
	}
	lock.holders++
	m.mapLock.Unlock()

	lock.mutex.Lock()

	return func() {
		lock.mutex.Unlock()

		m.mapLock.Lock()
		defer m.mapLock.Unlock()

		lock.holders--
		if lock.holders == 0 {
			delete(m.locks, key)
		}
	}
}
