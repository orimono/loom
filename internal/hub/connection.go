package hub

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type NodeCfg struct {
	pingInterval time.Duration
	pongTimeout  time.Duration
	writeTimeout time.Duration
}

type NodeConn struct {
	nodeID   string
	tenantID string
	conn     *websocket.Conn
	sendCh   chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
	nodeCfg  NodeCfg
	onClose  func(nodeID string)
	wg       sync.WaitGroup
}

func NewNodeConn(nodeID string, tenantID string, conn *websocket.Conn) *NodeConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &NodeConn{
		nodeID:   nodeID,
		tenantID: tenantID,
		conn:     conn,
		sendCh:   make(chan []byte, 256),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (c *NodeConn) start() {
	c.wg.Add(3)
	go c.readLoop()
	go c.writeLoop()
	go c.heartbeatLoop()
}

func (c *NodeConn) readLoop() {
	defer c.wg.Done()
	defer c.cancel()
	defer c.onClose(c.nodeID)

	c.conn.SetReadDeadline(time.Now().Add(c.nodeCfg.pongTimeout))

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("read error", "nodeID", c.nodeID, "error", err)
			}
			return
		}
		_ = messageType
		_ = data
	}
}

func (c *NodeConn) writeLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case data, ok := <-c.sendCh:
			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(c.nodeCfg.writeTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				slog.Error("write error", "nodeID", c.nodeID, "error", err)
				c.cancel()
				return
			}
		}
	}
}

func (c *NodeConn) heartbeatLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.nodeCfg.pingInterval)
	defer ticker.Stop()

	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.nodeCfg.pongTimeout))
		return nil
	})

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.nodeCfg.writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (c *NodeConn) closeGracefully() {
	c.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	c.cancel()
}

func (c *NodeConn) wait() {
	c.wg.Wait()
}
