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

// EnsureStream creates or updates the publisher's default stream.
func (p *Publisher) EnsureStream(ctx context.Context, subject string) error {
	_, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     p.streamName,
		Subjects: []string{subject},
	})
	return err
}

// EnsureNamedStream creates or updates a stream with an explicit name and subject filter.
func (p *Publisher) EnsureNamedStream(ctx context.Context, streamName, subject string) error {
	_, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
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

func (p *Publisher) PublishExecutorRegistered(ctx context.Context, nodeID string, exe ito.ExecutorRegistration) error {
	data, err := json.Marshal(exe)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("executor.registered.%s.%s", nodeID, exe.Kind)
	_, err = p.js.Publish(ctx, subject, data)
	return err
}
