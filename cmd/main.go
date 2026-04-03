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
	"github.com/orimono/ito"
	"github.com/orimono/loom/internal/api"
	"github.com/orimono/loom/internal/config"
	"github.com/orimono/loom/internal/hub"
	"github.com/orimono/loom/internal/node"
	"github.com/orimono/loom/internal/store"
	"github.com/orimono/loom/internal/telemetry"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func makeWSHandler(nodeRegistry *node.NodeRegistry, connRegistry *hub.ConnRegistry, telHub *telemetry.Hub, nodeCfg hub.NodeCfg) http.HandlerFunc {
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

		resp, _ := json.Marshal(map[string]string{"status": "accepted"})
		conn.WriteMessage(websocket.TextMessage, resp)

		nodeConn := hub.NewNodeConn(pkt.NodeID, "", conn, nodeCfg)
		nodeConn.OnMessage(func(data []byte) {
			var t ito.Telemetry
			if err := json.Unmarshal(data, &t); err != nil {
				return
			}
			telHub.Publish(t)
		})
		connRegistry.Register(pkt.NodeID, nodeConn)
	}
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

	nodeCfg := hub.NodeCfg{
		PingInterval: time.Duration(cfg.PingInterval),
		PongTimeout:  time.Duration(cfg.PongTimeout),
		WriteTimeout: time.Duration(cfg.WriteTimeout),
	}

	nodeStore := store.NewPostgresNodeStore(pool)
	nodeRegistry := node.NewNodeRegistry(nodeStore)
	connRegistry := hub.NewConnRegistry()
	telHub := telemetry.NewHub()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", makeWSHandler(nodeRegistry, connRegistry, telHub, nodeCfg))
	mux.HandleFunc("/api/nodes", api.NodesHandler(nodeRegistry))
	mux.HandleFunc("/api/stream", api.SSEHandler(telHub))

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
