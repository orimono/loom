package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/orimono/ito"
	"github.com/orimono/loom/internal/config"
	"github.com/orimono/loom/internal/hub"
	loomjs "github.com/orimono/loom/internal/jetstream"
	"github.com/orimono/loom/internal/node"
	"github.com/orimono/loom/internal/store"
	"github.com/orimono/loom/internal/telemetry"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func makeWSHandler(nodeRegistry *node.NodeRegistry, connRegistry *hub.ConnRegistry, telHub *telemetry.Hub, pub *loomjs.Publisher, nc *nats.Conn, nodeCfg hub.NodeCfg) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("upgrade failed", "err", err)
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			slog.Error("read failed", "err", err)
			conn.Close()
			return
		}

		var pkt ito.JoinPacket
		if err := json.Unmarshal(msg, &pkt); err != nil {
			slog.Warn("invalid JoinPacket", "err", err)
			conn.Close()
			return
		}

		n := &node.Node{
			JoinPacket: pkt,
			Status:     node.Online,
		}
		if err := nodeRegistry.Register(n); err != nil {
			slog.Error("register failed", "nodeID", pkt.NodeID, "err", err)
			conn.Close()
			return
		}

		slog.Info("node registered", "nodeID", pkt.NodeID, "hostname", pkt.Hostname)

		resp, _ := ito.Encode(ito.KindJoinAccepted, map[string]string{"status": "accepted"})
		conn.WriteMessage(websocket.TextMessage, resp)

		nodeConn := hub.NewNodeConn(pkt.NodeID, "", conn, nodeCfg)
		nodeConn.OnMessage(func(data []byte) {
			var t ito.Telemetry
			if err := json.Unmarshal(data, &t); err != nil {
				return
			}
			telHub.Publish(t)
			if pub != nil {
				pub.Publish(nodeConn.Context(), t)
			}
			if nc != nil {
				nc.Publish("orimono.live."+t.NodeID+"."+t.Type, data)
			}
		})
		connRegistry.Register(pkt.NodeID, nodeConn)
	}
}

// setupNATSRPC registers synchronous request/reply handlers so osa can query loom without HTTP.
func setupNATSRPC(nc *nats.Conn, nodeRegistry *node.NodeRegistry, telStore *store.TelemetryStore, connRegistry *hub.ConnRegistry) {
	nc.Subscribe("orimono.loom.nodes.list", func(msg *nats.Msg) {
		nodes := nodeRegistry.ListAll()
		resp := make([]node.NodeResponse, 0, len(nodes))
		for _, n := range nodes {
			resp = append(resp, node.NodeResponse{
				NodeID:     n.NodeID,
				Hostname:   n.Hostname,
				OS:         n.OS,
				Arch:       n.Arch,
				Tags:       n.Tags,
				Status:     n.Status,
				LastSeenAt: n.LastSeenAt.Format("2006-01-02 15:04:05"),
			})
		}
		data, _ := json.Marshal(resp)
		msg.Respond(data)
	})

	nc.Subscribe("orimono.loom.history", func(msg *nats.Msg) {
		var req struct {
			NodeID string `json:"node_id"`
			Type   string `json:"type"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			msg.Respond([]byte(`{"error":"invalid request"}`))
			return
		}
		if req.NodeID == "" || req.Type == "" {
			msg.Respond([]byte(`{"error":"node_id and type required"}`))
			return
		}
		if req.Limit <= 0 || req.Limit > 1440 {
			req.Limit = 120
		}
		records, err := telStore.Query(context.Background(), req.NodeID, req.Type, req.Limit)
		if err != nil {
			msg.Respond([]byte(`{"error":"query failed"}`))
			return
		}
		data, _ := json.Marshal(records)
		msg.Respond(data)
	})

	// nc.Subscribe("orimono.loom.executors", func(msg *nats.Msg) {
	// 	var req struct {
	// 		NodeID string `json:"node_id"`
	// 	}

	// 	if err := json.Unmarshal(msg.Data, &req); err != nil {
	// 		msg.Respond([]byte(`{"error":"invalid request"}`))
	// 		return
	// 	}

	// 	if req.NodeID == "" {
	// 		msg.Respond([]byte(`{"error":"node_id and type required"}`))
	// 		return
	// 	}
	// })

	nc.Subscribe("orimono.loom.executor.register", func(msg *nats.Msg) {
		var req struct {
			NodeID   string                   `json:"node_id"`
			Executor ito.ExecutorRegistration `json:"executor"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			msg.Respond([]byte(`{"error":"invalid request"}`))
			return
		}
		if req.NodeID == "" {
			msg.Respond([]byte(`{"error":"node_id required"}`))
			return
		}
		data, err := ito.Encode(ito.KindExecutorRegister, req.Executor)
		if err != nil {
			msg.Respond([]byte(`{"error":"encode failed"}`))
			return
		}
		if err := connRegistry.Send(req.NodeID, data); err != nil {
			msg.Respond([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		msg.Respond([]byte(`{"ok":true}`))
	})

	slog.Info("nats rpc handlers registered")
}

func main() {
	cfg := config.MustLoad()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	var nc *nats.Conn
	var pub *loomjs.Publisher

	if cfg.NatsURL != "" {
		nc, err = nats.Connect(cfg.NatsURL)
		if err != nil {
			slog.Warn("failed to connect to nats, running without nats", "err", err)
		} else {
			defer nc.Drain()
			streamName := cfg.StreamName
			if streamName == "" {
				streamName = "TELEMETRY"
			}
			p, err := loomjs.NewPublisher(nc, streamName)
			if err != nil {
				slog.Warn("failed to create jetstream publisher", "err", err)
			} else {
				if err := p.EnsureStream(ctx, "telemetry.>"); err != nil {
					slog.Warn("failed to ensure stream", "err", err)
				} else {
					pub = p
					slog.Info("jetstream publisher ready", "stream", streamName)
				}
			}
		}
	}

	nodeCfg := hub.NodeCfg{
		PingInterval: time.Duration(cfg.PingInterval),
		PongTimeout:  time.Duration(cfg.PongTimeout),
		WriteTimeout: time.Duration(cfg.WriteTimeout),
	}

	nodeStore := store.NewPostgresNodeStore(pool)
	nodeRegistry := node.NewNodeRegistry(nodeStore)
	connRegistry := hub.NewConnRegistry()
	telHub := telemetry.NewHub()
	telStore := store.NewTelemetryStore(pool)

	if nc != nil {
		setupNATSRPC(nc, nodeRegistry, telStore, connRegistry)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", makeWSHandler(nodeRegistry, connRegistry, telHub, pub, nc, nodeCfg))

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	slog.Info("loom listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
