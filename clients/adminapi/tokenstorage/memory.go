package tokenstorage

import (
	"context"
	"sync"
)

// InMemory is a TokenStorage that keeps tokens in the memory of the current
// process. Tokens are lost on restart and not shared between processes.
type InMemory struct {
	mu     sync.RWMutex
	tokens map[string]Token
}

// NewInMemory returns an empty in-memory storage.
func NewInMemory() *InMemory {
	return &InMemory{tokens: make(map[string]Token)}
}

// Get returns the token stored under key, or ErrNotFound.
func (s *InMemory) Get(_ context.Context, key string) (Token, error) {
	s.mu.RLock()
	token, ok := s.tokens[key]
	s.mu.RUnlock()

	if !ok {
		return Token{}, ErrNotFound
	}
	return token, nil
}

// Set stores token under key.
func (s *InMemory) Set(_ context.Context, key string, token Token) error {
	s.mu.Lock()
	s.tokens[key] = token
	s.mu.Unlock()
	return nil
}

// Delete removes the token stored under key.
func (s *InMemory) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.tokens, key)
	s.mu.Unlock()
	return nil
}
