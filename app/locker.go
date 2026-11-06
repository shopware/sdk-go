package app

import (
	"context"
	"sync"
)

// keyLocker is the default Locker of an App. Every Admin API client has its
// own deduplication of token fetches; keyLocker extends it to all clients the
// App creates, so clients of a shop that start at the same time fetch one
// token. It works within this process only; set Config.Locker to serialize
// fetches across instances.
type keyLocker struct {
	mu    sync.Mutex
	locks map[string]*keyLock
}

// keyLock is a mutex that can be abandoned when the context is done. refs
// counts holders and waiters, so unused keys are removed.
type keyLock struct {
	held chan struct{}
	refs int
}

func newKeyLocker() *keyLocker {
	return &keyLocker{locks: make(map[string]*keyLock)}
}

// Lock blocks until the lock for key is held or ctx is done.
func (l *keyLocker) Lock(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	lock, ok := l.locks[key]
	if !ok {
		lock = &keyLock{held: make(chan struct{}, 1)}
		l.locks[key] = lock
	}
	lock.refs++
	l.mu.Unlock()

	select {
	case lock.held <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-lock.held
				l.release(key, lock)
			})
		}, nil
	case <-ctx.Done():
		l.release(key, lock)
		return nil, ctx.Err()
	}
}

func (l *keyLocker) release(key string, lock *keyLock) {
	l.mu.Lock()
	lock.refs--
	if lock.refs == 0 {
		delete(l.locks, key)
	}
	l.mu.Unlock()
}
