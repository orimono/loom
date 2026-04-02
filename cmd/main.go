package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orimono/ito"
	"github.com/orimono/loom/internal/node"
	"github.com/orimono/loom/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func makeWSHandler(registry *node.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("upgrade failed", "err", err)
			return
		}
		defer conn.Close()

		_, msg, err := conn.ReadMessage()
		if err != nil {
			slog.Error("read failed", "err", err)
			return
		}

		var pkt ito.JoinPacket
		if err := json.Unmarshal(msg, &pkt); err != nil {
			slog.Warn("invalid JoinPacket", "err", err)
			return
		}

		n := &node.Node{
			JoinPacket: pkt,
			Status:     node.Pending,
		}
		if err := registry.Register(n); err != nil {
			slog.Error("register failed", "nodeID", pkt.NodeID, "err", err)
			return
		}

		slog.Info("node registered", "nodeID", pkt.NodeID, "hostname", pkt.Hostname, "os", pkt.OS, "arch", pkt.Arch)

		resp, _ := json.Marshal(map[string]string{"status": "accepted"})
		conn.WriteMessage(websocket.TextMessage, resp)
	}
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is not set")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	nodeStore := store.NewPostgresNodeStore(pool)
	registry := node.NewNodeRegistry(nodeStore)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", makeWSHandler(registry))

	srv := &http.Server{Addr: ":8080", Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	slog.Info("loom listening", "addr", ":8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
