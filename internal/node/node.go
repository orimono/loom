package node

import (
	"time"

	"github.com/orimono/ito"
)

type Status int

const (
	Pending Status = iota
	Online
	Reconnecting
	Offline
	Evicted
)

type Node struct {
	ito.JoinPacket
	Status     Status
	LastSeenAt time.Time
}

type NodeResponse struct {
	NodeID     string   `json:"node_id"`
	Hostname   string   `json:"hostname"`
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	Tags       []string `json:"tags"`
	Status     Status   `json:"status"`
	LastSeenAt string   `json:"last_seen_at"`
}
