package node

type NodeStore interface {
	Upsert(n *Node) error
	UpdateStatus(nodeID string, status Status) error
	Get(nodeID string) (*Node, error)
	List() ([]*Node, error)
}
