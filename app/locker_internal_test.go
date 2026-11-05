package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyLocker_SerializesOneKey(t *testing.T) {
	l := newKeyLocker()
	unlock, err := l.Lock(context.Background(), "shop-1")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = l.Lock(ctx, "shop-1")
	require.ErrorIs(t, err, context.DeadlineExceeded, "a second caller waits for the first")

	other, err := l.Lock(context.Background(), "shop-2")
	require.NoError(t, err, "other keys are not blocked")
	other()

	unlock()
	unlock() // a second call is a no-op

	again, err := l.Lock(context.Background(), "shop-1")
	require.NoError(t, err)
	again()
}

func TestKeyLocker_WaiterGetsLockAfterUnlock(t *testing.T) {
	l := newKeyLocker()
	unlock, err := l.Lock(context.Background(), "shop-1")
	require.NoError(t, err)

	acquired := make(chan struct{})
	go func() {
		next, lockErr := l.Lock(context.Background(), "shop-1")
		assert.NoError(t, lockErr)
		close(acquired)
		next()
	}()

	select {
	case <-acquired:
		t.Fatal("the lock was acquired while held")
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("the waiter did not get the lock")
	}
}

func TestKeyLocker_RemovesUnusedKeys(t *testing.T) {
	l := newKeyLocker()
	unlock, err := l.Lock(context.Background(), "shop-1")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = l.Lock(ctx, "shop-1")
	require.ErrorIs(t, err, context.Canceled)

	unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	assert.Empty(t, l.locks, "keys are removed when nobody holds or waits for them")
}
