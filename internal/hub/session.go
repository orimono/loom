package hub

import (
	"sync"

	"github.com/orimono/ito"
)

type Session struct {
	sessions   sync.Map
	nodeStatus sync.Map
	EventChan  chan ito.JoinPacket
}
