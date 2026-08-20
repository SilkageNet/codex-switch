package secretstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

func target(key string) string {
	return "codex-switch/" + key
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("secret key must not be empty")
	}
	return nil
}

type MemoryStore struct {
	mu     sync.RWMutex
	values map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: make(map[string]string)}
}

func (store *MemoryStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values[key] = value
	return nil
}

func (store *MemoryStore) Get(key string) (string, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	value, ok := store.values[key]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (store *MemoryStore) Delete(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.values[key]; !ok {
		return ErrNotFound
	}
	delete(store.values, key)
	return nil
}
