/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package snapshots provides an interface for managing raft snapshots.
package snapshots

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/raft"
	"golang.org/x/exp/slog"

	"gitlab.com/webmesh/node/pkg/meshdb/models/raftdb"
)

// Snapshotter is an interface for taking and restoring snapshots.
type Snapshotter interface {
	// Snapshot returns a new snapshot.
	Snapshot(ctx context.Context) (raft.FSMSnapshot, error)
	// Restore restores a snapshot.
	Restore(ctx context.Context, r io.ReadCloser) error
}

type snapshotter struct {
	db  *sql.DB
	log *slog.Logger
}

// New returns a new Snapshotter.
func New(db *sql.DB) Snapshotter {
	return &snapshotter{
		db:  db,
		log: slog.Default().With("component", "snapshots"),
	}
}

type snapshotModel struct {
	State     []raftdb.MeshState `json:"state"`
	Nodes     []raftdb.Node      `json:"nodes"`
	Leases    []raftdb.Lease     `json:"leases"`
	RaftACLs  []raftdb.RaftAcl   `json:"raft_acls"`
	NodeEdges []raftdb.NodeEdge  `json:"node_edges"`
}

func (s *snapshotter) Snapshot(ctx context.Context) (raft.FSMSnapshot, error) {
	s.log.Info("creating new db snapshot")
	start := time.Now()
	q := raftdb.New(s.db)
	var model snapshotModel
	var err error
	model.State, err = q.DumpMeshState(ctx)
	if err != nil {
		return nil, fmt.Errorf("dump mesh state: %w", err)
	}
	model.Nodes, err = q.DumpNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("dump nodes: %w", err)
	}
	model.Leases, err = q.DumpLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("dump leases: %w", err)
	}
	model.RaftACLs, err = q.DumpRaftACLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("dump raft acls: %w", err)
	}
	model.NodeEdges, err = q.DumpNodeEdges(ctx)
	if err != nil {
		return nil, fmt.Errorf("dump node edges: %w", err)
	}
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gzw).Encode(model); err != nil {
		return nil, fmt.Errorf("encode snapshot model: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	snapshot := &snapshot{&buf}
	s.log.Info("db snapshot complete",
		slog.String("duration", time.Since(start).String()),
		slog.String("size", snapshot.size()),
	)
	return snapshot, nil
}

func (s *snapshotter) Restore(ctx context.Context, r io.ReadCloser) error {
	defer r.Close()
	s.log.Info("restoring db snapshot")
	start := time.Now()
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()
	var model snapshotModel
	if err := json.NewDecoder(gzr).Decode(&model); err != nil {
		return fmt.Errorf("decode snapshot model: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	func() {
		err := tx.Rollback()
		if err != sql.ErrTxDone && err != nil {
			s.log.Error("rollback transaction", slog.String("error", err.Error()))
		}
	}()
	q := raftdb.New(tx)
	err = q.DropMeshState(ctx)
	if err != nil {
		return fmt.Errorf("drop mesh state: %w", err)
	}
	err = q.DropNodes(ctx)
	if err != nil {
		return fmt.Errorf("drop nodes: %w", err)
	}
	err = q.DropLeases(ctx)
	if err != nil {
		return fmt.Errorf("drop leases: %w", err)
	}
	err = q.DropRaftACLs(ctx)
	if err != nil {
		return fmt.Errorf("drop raft acls: %w", err)
	}
	err = q.DropNodeEdges(ctx)
	if err != nil {
		return fmt.Errorf("drop node edges: %w", err)
	}
	for _, state := range model.State {
		s.log.Debug("restoring mesh state", slog.Any("state", state))
		// nolint:gosimple
		err = q.RestoreMeshState(ctx, raftdb.RestoreMeshStateParams{
			Key:   state.Key,
			Value: state.Value,
		})
		if err != nil {
			return fmt.Errorf("restore mesh state: %w", err)
		}
	}
	for _, node := range model.Nodes {
		s.log.Debug("restoring node", slog.Any("node", node))
		// nolint:gosimple
		err = q.RestoreNode(ctx, raftdb.RestoreNodeParams{
			ID:             node.ID,
			PublicKey:      node.PublicKey,
			RaftPort:       node.RaftPort,
			GrpcPort:       node.GrpcPort,
			WireguardPort:  node.WireguardPort,
			PublicEndpoint: node.PublicEndpoint,
			NetworkIpv6:    node.NetworkIpv6,
			CreatedAt:      node.CreatedAt,
			UpdatedAt:      node.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore node: %w", err)
		}
	}
	for _, lease := range model.Leases {
		s.log.Debug("restoring lease", slog.Any("lease", lease))
		// nolint:gosimple
		err = q.RestoreLease(ctx, raftdb.RestoreLeaseParams{
			NodeID:    lease.NodeID,
			Ipv4:      lease.Ipv4,
			CreatedAt: lease.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore lease: %w", err)
		}
	}
	for _, acl := range model.RaftACLs {
		s.log.Debug("restoring raft acl", slog.Any("acl", acl))
		// nolint:gosimple
		err = q.RestoreRaftACL(ctx, raftdb.RestoreRaftACLParams{
			Name:      acl.Name,
			Nodes:     acl.Nodes,
			Action:    acl.Action,
			CreatedAt: acl.CreatedAt,
			UpdatedAt: acl.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore raft acl: %w", err)
		}
	}
	for _, edge := range model.NodeEdges {
		s.log.Debug("restoring node edge", slog.Any("edge", edge))
		// nolint:gosimple
		err = q.RestoreNodeEdge(ctx, raftdb.RestoreNodeEdgeParams{
			SrcNodeID: edge.SrcNodeID,
			DstNodeID: edge.DstNodeID,
			// TODO: Weight is not being restored, but it's not used anywhere yet.
		})
		if err != nil {
			return fmt.Errorf("restore node edge: %w", err)
		}
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	s.log.Info("restored db snapshot", slog.String("duration", time.Since(start).String()))
	return nil
}

// snapshot is a Raft snapshot.
type snapshot struct {
	data *bytes.Buffer
}

// Persist persists the snapshot to a sink.
func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	defer sink.Close()
	if s.data == nil {
		return fmt.Errorf("snapshot data is nil")
	}
	var buf bytes.Buffer
	if _, err := io.Copy(sink, io.TeeReader(s.data, &buf)); err != nil {
		return fmt.Errorf("write snapshot data to sink: %w", err)
	}
	s.data = &buf
	return nil
}

// Release releases the snapshot.
func (s *snapshot) Release() {
	s.data.Reset()
	s.data = nil
}

func (s *snapshot) size() string {
	b := int64(s.data.Len())
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}