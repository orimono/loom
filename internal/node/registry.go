package node

import (
	"sync"
)

type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	store NodeStore
}

func NewNodeRegistry(store NodeStore) *NodeRegistry {
	return nil
}
