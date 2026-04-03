package node

import (
	"sync"
	"time"
)

type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	store NodeStore
}

func NewNodeRegistry(store NodeStore) *NodeRegistry {
	return &NodeRegistry{
		nodes: make(map[string]*Node),
		store: store,
	}
}

func (r *NodeRegistry) Register(n *Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	n.LastSeenAt = time.Now()
	r.nodes[n.NodeID] = n

	return r.store.Upsert(n)
}

func (r *NodeRegistry) ListAll() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}
