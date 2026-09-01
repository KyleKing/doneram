package engine

import "sync"

// memo runs one call per distinct key and shares its result with every
// caller that asked for the same key, so a fleet config naming the same
// action in a dozen files costs one request.
type memo[K comparable, V any] struct {
	mu      sync.Mutex
	entries map[K]*memoEntry[V]
}

type memoEntry[V any] struct {
	once  sync.Once
	value V
	err   error
}

func newMemo[K comparable, V any]() *memo[K, V] {
	return &memo[K, V]{entries: make(map[K]*memoEntry[V])}
}

func (m *memo[K, V]) do(key K, fn func() (V, error)) (V, error) {
	m.mu.Lock()
	entry, ok := m.entries[key]
	if !ok {
		entry = &memoEntry[V]{}
		m.entries[key] = entry
	}
	m.mu.Unlock()

	entry.once.Do(func() { entry.value, entry.err = fn() })
	return entry.value, entry.err
}
