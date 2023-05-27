// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.18.0
// source: edges.sql

package raftdb

import (
	"context"
	"database/sql"
)

const DeleteNodeEdge = `-- name: DeleteNodeEdge :exec
DELETE FROM node_edges WHERE src_node_id = ? AND dst_node_id = ?
`

type DeleteNodeEdgeParams struct {
	SrcNodeID string `json:"src_node_id"`
	DstNodeID string `json:"dst_node_id"`
}

func (q *Queries) DeleteNodeEdge(ctx context.Context, arg DeleteNodeEdgeParams) error {
	_, err := q.db.ExecContext(ctx, DeleteNodeEdge, arg.SrcNodeID, arg.DstNodeID)
	return err
}

const GetNodeEdge = `-- name: GetNodeEdge :one
SELECT src_node_id, dst_node_id, weight, attrs FROM node_edges WHERE src_node_id = ? AND dst_node_id = ?
`

type GetNodeEdgeParams struct {
	SrcNodeID string `json:"src_node_id"`
	DstNodeID string `json:"dst_node_id"`
}

func (q *Queries) GetNodeEdge(ctx context.Context, arg GetNodeEdgeParams) (NodeEdge, error) {
	row := q.db.QueryRowContext(ctx, GetNodeEdge, arg.SrcNodeID, arg.DstNodeID)
	var i NodeEdge
	err := row.Scan(
		&i.SrcNodeID,
		&i.DstNodeID,
		&i.Weight,
		&i.Attrs,
	)
	return i, err
}

const InsertNodeEdge = `-- name: InsertNodeEdge :exec
INSERT INTO node_edges (src_node_id, dst_node_id, weight, attrs) VALUES (?, ?, ?, ?)
`

type InsertNodeEdgeParams struct {
	SrcNodeID string         `json:"src_node_id"`
	DstNodeID string         `json:"dst_node_id"`
	Weight    int64          `json:"weight"`
	Attrs     sql.NullString `json:"attrs"`
}

func (q *Queries) InsertNodeEdge(ctx context.Context, arg InsertNodeEdgeParams) error {
	_, err := q.db.ExecContext(ctx, InsertNodeEdge,
		arg.SrcNodeID,
		arg.DstNodeID,
		arg.Weight,
		arg.Attrs,
	)
	return err
}

const ListNodeEdges = `-- name: ListNodeEdges :many
SELECT src_node_id, dst_node_id, weight, attrs FROM node_edges
`

func (q *Queries) ListNodeEdges(ctx context.Context) ([]NodeEdge, error) {
	rows, err := q.db.QueryContext(ctx, ListNodeEdges)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NodeEdge
	for rows.Next() {
		var i NodeEdge
		if err := rows.Scan(
			&i.SrcNodeID,
			&i.DstNodeID,
			&i.Weight,
			&i.Attrs,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const NodeEdgeExists = `-- name: NodeEdgeExists :one
SELECT 1 FROM node_edges WHERE src_node_id = ? AND dst_node_id = ?
`

type NodeEdgeExistsParams struct {
	SrcNodeID string `json:"src_node_id"`
	DstNodeID string `json:"dst_node_id"`
}

func (q *Queries) NodeEdgeExists(ctx context.Context, arg NodeEdgeExistsParams) (int64, error) {
	row := q.db.QueryRowContext(ctx, NodeEdgeExists, arg.SrcNodeID, arg.DstNodeID)
	var column_1 int64
	err := row.Scan(&column_1)
	return column_1, err
}

const NodeHasEdges = `-- name: NodeHasEdges :one
SELECT 1 FROM node_edges WHERE src_node_id = ? OR dst_node_id = ? LIMIT 1
`

type NodeHasEdgesParams struct {
	SrcNodeID string `json:"src_node_id"`
	DstNodeID string `json:"dst_node_id"`
}

func (q *Queries) NodeHasEdges(ctx context.Context, arg NodeHasEdgesParams) (int64, error) {
	row := q.db.QueryRowContext(ctx, NodeHasEdges, arg.SrcNodeID, arg.DstNodeID)
	var column_1 int64
	err := row.Scan(&column_1)
	return column_1, err
}

const UpdateNodeEdge = `-- name: UpdateNodeEdge :exec
UPDATE node_edges SET weight = ?, attrs = ? WHERE src_node_id = ? AND dst_node_id = ?
`

type UpdateNodeEdgeParams struct {
	Weight    int64          `json:"weight"`
	Attrs     sql.NullString `json:"attrs"`
	SrcNodeID string         `json:"src_node_id"`
	DstNodeID string         `json:"dst_node_id"`
}

func (q *Queries) UpdateNodeEdge(ctx context.Context, arg UpdateNodeEdgeParams) error {
	_, err := q.db.ExecContext(ctx, UpdateNodeEdge,
		arg.Weight,
		arg.Attrs,
		arg.SrcNodeID,
		arg.DstNodeID,
	)
	return err
}
