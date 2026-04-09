package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orimono/ito"
)

type TelemetryStore struct {
	pool *pgxpool.Pool
}

func NewTelemetryStore(pool *pgxpool.Pool) *TelemetryStore {
	return &TelemetryStore{pool: pool}
}

// Query returns up to limit telemetry records for the given node and type, ordered by ts ASC.
func (s *TelemetryStore) Query(ctx context.Context, nodeID, typ string, limit int) ([]ito.Telemetry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT node_id, type, ts, payload
		FROM (
			SELECT node_id, type, ts, payload
			FROM telemetry
			WHERE node_id = $1 AND type = $2
			ORDER BY ts DESC
			LIMIT $3
		) sub
		ORDER BY ts ASC
	`, nodeID, typ, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ito.Telemetry, 0)
	for rows.Next() {
		var (
			t          ito.Telemetry
			payloadRaw string
		)
		if err := rows.Scan(&t.NodeID, &t.Type, &t.Timestamp, &payloadRaw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payloadRaw), &t.Payload); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
