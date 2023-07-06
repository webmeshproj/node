/*
Copyright 2023 Avi Zimmerman <avi.zimmerman@gmail.com>

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

package state

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/webmeshproj/node/pkg/meshdb"
	"github.com/webmeshproj/node/pkg/meshdb/models"
)

var (
	ipv6Prefix = "fd00:dead::/48"
	ipv4Prefix = "172.16.0.0/12"

	publicNode  = "public"
	privateNode = "private"

	publicNodePublicAddr = "1.1.1.1"

	publicNodePrivateAddr  = "172.16.0.1"
	privateNodePrivateAddr = "172.16.0.2"

	rpcPort = 1
)

func TestGetIPv6Prefix(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()
	prefix, err := state.GetIPv6Prefix(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prefix.String() != ipv6Prefix {
		t.Fatalf("expected %s, got %s", ipv6Prefix, prefix)
	}
}

func TestGetIPv4Prefix(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()
	prefix, err := state.GetIPv4Prefix(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prefix.String() != ipv4Prefix {
		t.Fatalf("expected %s, got %s", ipv4Prefix, prefix)
	}
}

func TestGetNodePrivateRPCAddress(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()
	// Node with public address
	addr, err := state.GetNodePrivateRPCAddress(context.Background(), "public")
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "172.16.0.1:1" {
		t.Errorf("expected '172.16.0.1:1', got %s", addr)
	}
	// Node with private address
	addr, err = state.GetNodePrivateRPCAddress(context.Background(), "private")
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "172.16.0.2:1" {
		t.Errorf("expected '172.16.0.2:1', got %s", addr)
	}
}

func TestListPublicRPCAddresses(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()

	addrs, err := state.ListPublicRPCAddresses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 {
		t.Errorf("expected 1 address, got %d", len(addrs))
	}
	for _, addr := range addrs {
		if addr.String() != "1.1.1.1:1" {
			t.Errorf("expected '1.1.1.1:1', got %s", addr)
		}
	}
}

func TestListPeerPublicRPCAddresses(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()

	// The private node should have the public node as a public peer
	addrs, err := state.ListPeerPublicRPCAddresses(context.Background(), privateNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 {
		t.Errorf("expected 1 address, got %d", len(addrs))
	}
	for _, addr := range addrs {
		if addr.String() != "1.1.1.1:1" {
			t.Errorf("expected '1.1.1.1:1', got %s", addr)
		}
	}

	// The public node should have no public peers
	addrs, err = state.ListPeerPublicRPCAddresses(context.Background(), publicNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 0 {
		t.Errorf("expected 0 addresses, got %d", len(addrs))
	}
}

func TestListPeerPrivateRPCAddresses(t *testing.T) {
	t.Parallel()

	state, teardown := setupTest(t)
	defer teardown()

	// The private node should have the public node as a private peer
	addrs, err := state.ListPeerPrivateRPCAddresses(context.Background(), privateNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 {
		t.Errorf("expected 1 address, got %d", len(addrs))
	}
	for id, addr := range addrs {
		if id != publicNode {
			t.Errorf("expected peer id %s, got %s", publicNode, id)
		}
		if addr.String() != "172.16.0.1:1" {
			t.Errorf("expected '172.16.0.1:1', got %s", addr)
		}
	}

	// Reverse for the public node
	addrs, err = state.ListPeerPrivateRPCAddresses(context.Background(), publicNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 {
		t.Errorf("expected 1 address, got %d", len(addrs))
	}
	for id, addr := range addrs {
		if id != privateNode {
			t.Errorf("expected peer id %s, got %s", privateNode, id)
		}
		if addr.String() != "172.16.0.2:1" {
			t.Errorf("expected '172.16.0.2:1', got %s", addr)
		}
	}
}

func setupTest(t *testing.T) (*state, func()) {
	t.Helper()
	db, close, err := meshdb.NewTestDB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := models.New(db.Write())
	err = q.SetIPv6Prefix(ctx, ipv6Prefix)
	if err != nil {
		t.Fatal(err)
	}
	err = q.SetIPv4Prefix(ctx, ipv4Prefix)
	if err != nil {
		t.Fatal(err)
	}
	// Node with public address
	_, err = q.InsertNode(ctx, models.InsertNodeParams{
		ID: publicNode,
		PublicKey: sql.NullString{
			String: "public",
			Valid:  true,
		},
		PrimaryEndpoint: sql.NullString{
			String: publicNodePublicAddr,
			Valid:  true,
		},
		GrpcPort:  int64(rpcPort),
		RaftPort:  2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Node with private address
	_, err = q.InsertNode(ctx, models.InsertNodeParams{
		ID: privateNode,
		PublicKey: sql.NullString{
			String: "private",
			Valid:  true,
		},
		GrpcPort:  int64(rpcPort),
		RaftPort:  2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Leases for each
	_, err = q.InsertNodeLease(ctx, models.InsertNodeLeaseParams{
		NodeID: publicNode,
		Ipv4: sql.NullString{
			String: publicNodePrivateAddr,
			Valid:  true,
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Leases for each
	_, err = q.InsertNodeLease(ctx, models.InsertNodeLeaseParams{
		NodeID: privateNode,
		Ipv4: sql.NullString{
			String: privateNodePrivateAddr,
			Valid:  true,
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db)
	return s.(*state), close
}