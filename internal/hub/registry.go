package hub

import (
	"errors"
	"log/slog"
	"sync"
)

type ConnRegistry struct {
	connMap sync.Map
}

func NewConnRegistry() *ConnRegistry {
	return &ConnRegistry{}
}

func (r *ConnRegistry) Register(nodeID string, conn *NodeConn) {
	conn.onClose = func(nodeID string) {
		r.Unregister(nodeID, conn)
	}
	if old, loaded := r.connMap.Swap(nodeID, conn); loaded {
		slog.Warn("Connection replaced on registry: ", "nodeID", nodeID)
		oldConn := old.(*NodeConn)
		go func() {
			oldConn.closeGracefully()
			oldConn.wait()
		}()
	}
	conn.start()
}

func (r *ConnRegistry) Unregister(nodeID string, conn *NodeConn) {
	r.connMap.CompareAndDelete(nodeID, conn)
}

func (r *ConnRegistry) Send(nodeID string, msg []byte) error {
	conn, loaded := r.connMap.Load(nodeID)
	if !loaded {
		return errors.New("Node not found")
	}
	nodeConn := conn.(*NodeConn)
	select {
	case nodeConn.sendCh <- msg:
	default:
		return errors.New("send buffer full")
	}

	return nil
}
