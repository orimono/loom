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
