package telemetry

import (
	"sync"

	"github.com/orimono/ito"
)

type SubKey struct {
	NodeID string
	Type   string
}

type Hub struct {
	mu   sync.RWMutex
	subs map[SubKey][]chan ito.Telemetry
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[SubKey][]chan ito.Telemetry),
	}
}

func (h *Hub) Subscribe(key SubKey) (<-chan ito.Telemetry, func()) {
	ch := make(chan ito.Telemetry, 16)

	h.mu.Lock()
	h.subs[key] = append(h.subs[key], ch)
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.subs[key]
		for i, s := range subs {
			if s == ch {
				h.subs[key] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (h *Hub) Publish(t ito.Telemetry) {
	key := SubKey{NodeID: t.NodeID, Type: t.Type}

	h.mu.RLock()
	subs := h.subs[key]
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- t:
		default:
		}
	}
}
