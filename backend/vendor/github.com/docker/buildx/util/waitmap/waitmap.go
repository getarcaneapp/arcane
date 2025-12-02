package waitmap

import (
	"context"
	"sync"
)

type Map struct {
	mu sync.RWMutex
	m  map[string]any
	ch map[string]chan struct{}
}

func New() *Map {
	return &Map{
		m:  make(map[string]any),
		ch: make(map[string]chan struct{}),
	}
}

func (m *Map) Set(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.m[key] = value

	if ch, ok := m.ch[key]; ok {
		if ch != nil {
			close(ch)
		}
	}
	m.ch[key] = nil
}

func (m *Map) Get(ctx context.Context, keys ...string) (map[string]any, error) {
	if len(keys) == 0 {
		return map[string]any{}, nil
	}

	if len(keys) > 1 {
		out := make(map[string]any)
		for _, key := range keys {
			mm, err := m.Get(ctx, key)
			if err != nil {
				return nil, err
			}
			out[key] = mm[key]
		}
		return out, nil
	}

	key := keys[0]
	m.mu.Lock()
	ch, ok := m.ch[key]
	if !ok {
		ch = make(chan struct{})
		m.ch[key] = ch
	}

	if ch != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-ch:
			m.mu.Lock()
		}
	}

	res := m.m[key]
	m.mu.Unlock()

	return map[string]any{key: res}, nil
}
