package jetstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/orimono/ito"
)

type Publisher struct {
	js         jetstream.JetStream
	streamName string
}

func NewPublisher(nc *nats.Conn, streamName string) (*Publisher, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return &Publisher{js: js, streamName: streamName}, nil
}

// EnsureStream creates the stream if it doesn't exist.
func (p *Publisher) EnsureStream(ctx context.Context, subject string) error {
	_, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     p.streamName,
		Subjects: []string{subject},
	})
	return err
}

func (p *Publisher) Publish(ctx context.Context, t ito.Telemetry) {
	data, err := json.Marshal(t)
	if err != nil {
		slog.Warn("jetstream: failed to marshal telemetry", "err", err)
		return
	}

	subject := fmt.Sprintf("telemetry.%s.%s", t.NodeID, t.Type)
	if _, err := p.js.Publish(ctx, subject, data); err != nil {
		slog.Warn("jetstream: failed to publish", "subject", subject, "err", err)
	}
}
