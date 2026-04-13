package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orimono/ito"
)

type PostgresNodeExecutorStore struct {
	pool *pgxpool.Pool
}

type ExecutorRecord struct {
	ito.ExecutorRegistration
	createdAt time.Time
	updatedAt time.Time
}

func NewPostgresNodeExecutor(pool *pgxpool.Pool) *PostgresNodeExecutorStore {
	return &PostgresNodeExecutorStore{pool: pool}
}

func (s *PostgresNodeExecutorStore) Get(nodeId string) (*ExecutorRecord, error) {
	var executors ExecutorRecord
	row := s.pool.QueryRow(context.Background(), `
	SELECT executors from node_executors where node_id = $1
	`, nodeId)

	err := row.Scan(&executors)
	if err != nil {
		return nil, err
	}

	return &executors, nil
}

func (s *PostgresNodeExecutorStore) Upsert(nodeId string, executors []ito.ExecutorRegistration) error {
	executorsJSON, err := json.Marshal(executors)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO node_executors (node_id, executors)
		VALUES ($1, $2)
		ON CONFLICT (node_id) DO UPDATE SET executors = EXCLUDED.executors
	`, executorsJSON, nodeId)

	return err
}
