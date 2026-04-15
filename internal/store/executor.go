package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orimono/ito"
)

type PostgresNodeExecutorStore struct {
	pool *pgxpool.Pool
}

type ExecutorRecord struct {
	ito.ExecutorRegistration
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

func NewPostgresNodeExecutor(pool *pgxpool.Pool) *PostgresNodeExecutorStore {
	return &PostgresNodeExecutorStore{pool: pool}
}

func scanExecutorRecord(row pgx.Row) (*ExecutorRecord, error) {
	var (
		executorJSON []byte
		createdAt    time.Time
		updatedAt    *time.Time
	)
	if err := row.Scan(&executorJSON, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	var executor ExecutorRecord
	if err := json.Unmarshal(executorJSON, &executor); err != nil {
		return nil, err
	}

	executor.CreatedAt = createdAt
	executor.UpdatedAt = updatedAt

	return &executor, nil
}

func (s *PostgresNodeExecutorStore) Get(ctx context.Context, nodeId string) ([]*ExecutorRecord, error) {
	executors := make([]*ExecutorRecord, 0)
	rows, err := s.pool.Query(ctx, `
		SELECT executor, created_at, updated_at FROM node_executors WHERE node_id = $1
	`, nodeId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		exe, err := scanExecutorRecord(rows)
		if err != nil {
			return nil, err
		}
		executors = append(executors, exe)
	}
	return executors, rows.Err()
}

func (s *PostgresNodeExecutorStore) Upsert(ctx context.Context, nodeId string, executors []ito.ExecutorRegistration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `DELETE FROM node_executors WHERE node_id = $1`, nodeId); err != nil {
		return err
	}

	for _, exe := range executors {
		exeJSON, err := json.Marshal(exe)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO node_executors (node_id, kind, executor) VALUES ($1, $2, $3)
		`, nodeId, exe.Kind, exeJSON); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresNodeExecutorStore) UpsertOne(ctx context.Context, nodeId string, exe ito.ExecutorRegistration) error {
	exeJSON, err := json.Marshal(exe)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO node_executors (node_id, kind, executor)
		VALUES ($1, $2, $3)
		ON CONFLICT (node_id, kind) DO UPDATE
			SET executor = EXCLUDED.executor, updated_at = NOW()
	`, nodeId, exe.Kind, exeJSON)
	return err
}
