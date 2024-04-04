package concache

import (
	"sync"
)

type keyedMutex struct {
	locks   map[string]*sync.Mutex
	mapLock *sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[string]*sync.Mutex), mapLock: &sync.Mutex{}}
}

func (m *keyedMutex) Lock(key string) func() {
	m.mapLock.Lock()
	lock, found := m.locks[key]
	if !found {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	m.mapLock.Unlock()

	lock.Lock()
	return func() { lock.Unlock() }
}

func (m *keyedMutex) Delete(key string) {
	m.mapLock.Lock()
	defer m.mapLock.Unlock()

	delete(m.locks, key)
}
