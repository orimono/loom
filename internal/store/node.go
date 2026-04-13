package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orimono/loom/internal/node"
)

type PostgresNodeStore struct {
	pool *pgxpool.Pool
}

func NewPostgresNodeStore(pool *pgxpool.Pool) *PostgresNodeStore {
	return &PostgresNodeStore{pool: pool}
}

func (s *PostgresNodeStore) Upsert(n *node.Node) error {
	taskManifestJSON, err := json.Marshal(n.TaskManifest)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO nodes (node_id, hostname, os, arch, task_manifest, version, tags, status, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (node_id) DO UPDATE SET
			hostname      = EXCLUDED.hostname,
			os            = EXCLUDED.os,
			arch          = EXCLUDED.arch,
			task_manifest = EXCLUDED.task_manifest,
			version       = EXCLUDED.version,
			tags          = EXCLUDED.tags,
			status        = EXCLUDED.status,
			last_seen_at  = EXCLUDED.last_seen_at
	`, n.NodeID, n.Hostname, n.OS, n.Arch, taskManifestJSON, n.Version, n.Tags, int(n.Status), n.LastSeenAt)
	return err
}

func (s *PostgresNodeStore) UpdateStatus(nodeID string, status node.Status) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE nodes SET status = $1 WHERE node_id = $2
	`, int(status), nodeID)
	return err
}

func (s *PostgresNodeStore) Get(nodeID string) (*node.Node, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT node_id, hostname, os, arch, task_manifest, version, tags, status, last_seen_at
		FROM nodes WHERE node_id = $1
	`, nodeID)
	return scanNode(row)
}

func (s *PostgresNodeStore) List() ([]*node.Node, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT node_id, hostname, os, arch, task_manifest, version, tags, status, last_seen_at
		FROM nodes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*node.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(s scanner) (*node.Node, error) {
	var (
		n               node.Node
		taskManifestRaw []byte
		status          int
	)
	err := s.Scan(
		&n.NodeID, &n.Hostname, &n.OS, &n.Arch,
		&taskManifestRaw, &n.Version, &n.Tags,
		&status, &n.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(taskManifestRaw, &n.TaskManifest); err != nil {
		return nil, err
	}
	n.Status = node.Status(status)
	return &n, nil
}
