package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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

// pendingRegistry correlates outbound executor register requests with their
// WebSocket responses using a correlation ID.
type pendingRegistry struct {
	mu      sync.Mutex
	pending map[string]chan ito.ExecutorRegisteredResult
}

func newPendingRegistry() *pendingRegistry {
	return &pendingRegistry{pending: make(map[string]chan ito.ExecutorRegisteredResult)}
}

func (p *pendingRegistry) register(correlationID string) chan ito.ExecutorRegisteredResult {
	ch := make(chan ito.ExecutorRegisteredResult, 1)
	p.mu.Lock()
	p.pending[correlationID] = ch
	p.mu.Unlock()
	return ch
}

func (p *pendingRegistry) resolve(result ito.ExecutorRegisteredResult) {
	if result.CorrelationID == "" {
		return
	}
	p.mu.Lock()
	ch, ok := p.pending[result.CorrelationID]
	if ok {
		delete(p.pending, result.CorrelationID)
	}
	p.mu.Unlock()
	if ok {
		ch <- result
	}
}

type HandlerDeps struct {
	nodeRegistry *node.NodeRegistry
	connRegistry *hub.ConnRegistry
	telHub       *telemetry.Hub
	pub          *loomjs.Publisher
	nc           *nats.Conn
	nodeCfg      hub.NodeCfg
	pending      *pendingRegistry
}

func makeWSHandler(deps HandlerDeps) http.HandlerFunc {
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
		if err := deps.nodeRegistry.Register(n); err != nil {
			slog.Error("register failed", "nodeID", pkt.NodeID, "err", err)
			conn.Close()
			return
		}

		slog.Info("node registered", "nodeID", pkt.NodeID, "hostname", pkt.Hostname)

		resp, _ := ito.Encode(ito.KindJoinAccepted, map[string]string{"status": "accepted"})
		conn.WriteMessage(websocket.TextMessage, resp)

		nodeConn := hub.NewNodeConn(pkt.NodeID, "", conn, deps.nodeCfg)
		nodeConn.OnMessage(func(data []byte) {
			// Try envelope first to route control messages.
			if env, err := ito.Decode(data); err == nil {
				switch env.Kind {
				case ito.KindExecutorRegistered:
					var result ito.ExecutorRegisteredResult
					if err := json.Unmarshal(env.Payload, &result); err == nil {
						deps.pending.resolve(result)
					}
					return
				}
			}

			var t ito.Telemetry
			if err := json.Unmarshal(data, &t); err != nil {
				return
			}
			deps.telHub.Publish(t)
			if deps.pub != nil {
				deps.pub.Publish(nodeConn.Context(), t)
			}
			if deps.nc != nil {
				deps.nc.Publish("orimono.live."+t.NodeID+"."+t.Type, data)
			}
		})
		deps.connRegistry.Register(pkt.NodeID, nodeConn)
	}
}

type RPCDeps struct {
	NodeRegistry  *node.NodeRegistry
	ConnRegistry  *hub.ConnRegistry
	ExecutorStore *store.PostgresNodeExecutorStore
	Pub           *loomjs.Publisher
	Pending       *pendingRegistry
}

// setupNATSRPC registers synchronous request/reply handlers so osa can query loom without HTTP.
func setupNATSRPC(nc *nats.Conn, deps RPCDeps) {
	nc.Subscribe("orimono.loom.nodes.list", func(msg *nats.Msg) {
		nodes := deps.NodeRegistry.ListAll()
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


	nc.Subscribe("orimono.loom.executors", func(msg *nats.Msg) {
		var req struct {
			NodeID string `json:"node_id"`
		}

		if err := json.Unmarshal(msg.Data, &req); err != nil {
			msg.Respond([]byte(`{"error":"invalid request"}`))
			return
		}

		if req.NodeID == "" {
			msg.Respond([]byte(`{"error":"node_id and type required"}`))
			return
		}

		records, err := deps.ExecutorStore.Get(context.Background(), req.NodeID)
		if err != nil {
			slog.Error("executor store get failed", "node_id", req.NodeID, "err", err)
			msg.Respond([]byte(`{"error":"query failed"}`))
			return
		}
		data, _ := json.Marshal(records)
		msg.Respond(data)
	})

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

		req.Executor.CorrelationID = uuid.NewString()
		ch := deps.Pending.register(req.Executor.CorrelationID)

		data, err := ito.Encode(ito.KindExecutorRegister, req.Executor)
		if err != nil {
			msg.Respond([]byte(`{"error":"encode failed"}`))
			return
		}
		if err := deps.ConnRegistry.Send(req.NodeID, data); err != nil {
			msg.Respond([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}

		// Wait for shutter to confirm persistence, then publish JetStream.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		select {
		case <-ctx.Done():
			msg.Respond([]byte(`{"error":"node did not respond in time"}`))
			return
		case result := <-ch:
			if !result.Success {
				errJSON, _ := json.Marshal(map[string]string{"error": result.Error})
				msg.Respond(errJSON)
				return
			}
		}

		if deps.Pub != nil {
			if err := deps.Pub.PublishExecutorRegistered(context.Background(), req.NodeID, req.Executor); err != nil {
				slog.Warn("failed to publish executor.registered to jetstream", "err", err)
			}
		}

		msg.Respond([]byte(`{"accepted":true}`))
	})

	slog.Info("nats rpc handlers registered")
}

func startExecutorConsumer(ctx context.Context, nc *nats.Conn, executorStore *store.PostgresNodeExecutorStore) {
	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("executor consumer: failed to create jetstream context", "err", err)
		return
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, "EXECUTOR", jetstream.ConsumerConfig{
		Durable:       "loom-executor-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: "executor.registered.>",
	})
	if err != nil {
		slog.Error("executor consumer: failed to create consumer", "err", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := cons.Fetch(10, jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			continue
		}

		for msg := range msgs.Messages() {
			var exe ito.ExecutorRegistration
			if err := json.Unmarshal(msg.Data(), &exe); err != nil {
				slog.Warn("executor consumer: failed to unmarshal", "err", err)
				msg.Nak()
				continue
			}

			// Extract nodeID from subject: executor.registered.{nodeID}.{kind}
			parts := splitSubject(msg.Subject())
			if len(parts) < 4 {
				slog.Warn("executor consumer: unexpected subject", "subject", msg.Subject())
				msg.Nak()
				continue
			}
			nodeID := parts[2]

			if err := executorStore.UpsertOne(ctx, nodeID, exe); err != nil {
				slog.Error("executor consumer: failed to upsert", "node_id", nodeID, "kind", exe.Kind, "err", err)
				msg.Nak()
				continue
			}

			// Notify SSE subscribers.
			nc.Publish("orimono.live."+nodeID+".executor", msg.Data())

			msg.Ack()
			slog.Info("executor persisted", "node_id", nodeID, "kind", exe.Kind)
		}
	}
}

func splitSubject(subject string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(subject); i++ {
		if i == len(subject) || subject[i] == '.' {
			parts = append(parts, subject[start:i])
			start = i + 1
		}
	}
	return parts
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
	executorStore := store.NewPostgresNodeExecutor(pool)
	pending := newPendingRegistry()

	if nc != nil {
		setupNATSRPC(nc, RPCDeps{
			NodeRegistry:  nodeRegistry,
			ConnRegistry:  connRegistry,
			ExecutorStore: executorStore,
			Pub:           pub,
			Pending:       pending,
		})

		if pub != nil {
			if err := pub.EnsureNamedStream(ctx, "EXECUTOR", "executor.registered.>"); err != nil {
				slog.Warn("failed to ensure executor stream", "err", err)
			} else {
				go startExecutorConsumer(ctx, nc, executorStore)
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", makeWSHandler(HandlerDeps{
		nodeRegistry: nodeRegistry,
		connRegistry: connRegistry,
		telHub:       telHub,
		pub:          pub,
		nc:           nc,
		nodeCfg:      nodeCfg,
		pending:      pending,
	}))

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
