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

package mesh

import (
	"context"
	"net/netip"
	"reflect"
	"sort"
	"testing"

	v1 "github.com/webmeshproj/api/v1"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/webmeshproj/node/pkg/meshdb/networking"
	"github.com/webmeshproj/node/pkg/meshdb/peers"
	"github.com/webmeshproj/node/pkg/storage"
)

func TestWireGuardPeers(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name    string
		peers   map[string][]string            // peerID -> addressv4 + addressv6
		edges   map[string][]string            // peerID -> []peerID
		wantIPs map[string]map[string][]string // peerID -> peerID -> []allowed ips
	}{
		{
			name: "simple 1-to-1",
			peers: map[string][]string{
				"peer1": {"172.16.0.1/32", "2001:db8::1/128"},
				"peer2": {"172.16.0.2/32", "2001:db8::2/128"},
			},
			edges: map[string][]string{
				"peer1": {"peer2"},
			},
			wantIPs: map[string]map[string][]string{
				"peer1": {
					"peer2": {"172.16.0.2/32", "2001:db8::2/128"},
				},
				"peer2": {
					"peer1": {"172.16.0.1/32", "2001:db8::1/128"},
				},
			},
		},
		{
			name: "simple 1-to-1-to-1",
			peers: map[string][]string{
				"peer1": {"172.16.0.1/32", "2001:db8::1/128"},
				"peer2": {"172.16.0.2/32", "2001:db8::2/128"},
				"peer3": {"172.16.0.3/32", "2001:db8::3/128"},
			},
			edges: map[string][]string{
				"peer1": {"peer2", "peer3"},
				"peer2": {"peer1", "peer3"},
				"peer3": {"peer1", "peer2"},
			},
			wantIPs: map[string]map[string][]string{
				"peer1": {
					"peer2": {"172.16.0.2/32", "2001:db8::2/128"},
					"peer3": {"172.16.0.3/32", "2001:db8::3/128"},
				},
				"peer2": {
					"peer1": {"172.16.0.1/32", "2001:db8::1/128"},
					"peer3": {"172.16.0.3/32", "2001:db8::3/128"},
				},
				"peer3": {
					"peer1": {"172.16.0.1/32", "2001:db8::1/128"},
					"peer2": {"172.16.0.2/32", "2001:db8::2/128"},
				},
			},
		},
		{
			name: "simple star",
			peers: map[string][]string{
				"router": {"172.16.0.1/32", "2001:db8::1/128"},
				"peer1":  {"172.16.0.2/32", "2001:db8::2/128"},
				"peer2":  {"172.16.0.3/32", "2001:db8::3/128"},
				"peer3":  {"172.16.0.4/32", "2001:db8::4/128"},
				"peer4":  {"172.16.0.5/32", "2001:db8::5/128"},
				"peer5":  {"172.16.0.6/32", "2001:db8::6/128"},
			},
			edges: map[string][]string{
				"router": {"peer1", "peer2", "peer3", "peer4", "peer5"},
			},
			wantIPs: map[string]map[string][]string{
				"router": {
					// Router should have all peers as allowed IPs
					"peer1": {"172.16.0.2/32", "2001:db8::2/128"},
					"peer2": {"172.16.0.3/32", "2001:db8::3/128"},
					"peer3": {"172.16.0.4/32", "2001:db8::4/128"},
					"peer4": {"172.16.0.5/32", "2001:db8::5/128"},
					"peer5": {"172.16.0.6/32", "2001:db8::6/128"},
				},
				// Peers should have all other peers as allowed IPs via the router
				"peer1": {
					"router": {
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
				},
				"peer2": {
					"router": {
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
				},
				"peer3": {
					"router": {
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
				},
				"peer4": {
					"router": {
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
				},
				"peer5": {
					"router": {
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
					},
				},
			},
		},
		{
			name: "simple site-to-site",
			peers: map[string][]string{
				"site1-router":   {"172.16.0.1/32", "2001:db8::1/128"},
				"site2-router":   {"172.16.0.2/32", "2001:db8::2/128"},
				"site1-follower": {"172.16.0.3/32", "2001:db8::3/128"},
				"site2-follower": {"172.16.0.4/32", "2001:db8::4/128"},
			},
			edges: map[string][]string{
				"site1-router": {"site2-router", "site1-follower"},
				"site2-router": {"site1-router", "site2-follower"},
			},
			wantIPs: map[string]map[string][]string{
				"site1-router": {
					"site2-router": {
						"172.16.0.2/32", "2001:db8::2/128",
						// site2-follower is reachable via site2-router
						"172.16.0.4/32", "2001:db8::4/128",
					},
					"site1-follower": {"172.16.0.3/32", "2001:db8::3/128"},
				},
				"site2-router": {
					"site1-router": {
						"172.16.0.1/32", "2001:db8::1/128",
						// site1-follower is reachable via site1-router
						"172.16.0.3/32", "2001:db8::3/128",
					},
					"site2-follower": {"172.16.0.4/32", "2001:db8::4/128"},
				},
				"site1-follower": {
					"site1-router": {
						// All IPs reachable via site1-router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.4/32", "2001:db8::4/128",
					},
				},
				"site2-follower": {
					"site2-router": {
						// All IPs reachable via site2-router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
					},
				},
			},
		},
		{
			name: "simple site-to-site-to-site",
			peers: map[string][]string{
				"site1-router":     {"172.16.0.1/32", "2001:db8::1/128"},
				"site2-router":     {"172.16.0.2/32", "2001:db8::2/128"},
				"site3-router":     {"172.16.0.3/32", "2001:db8::3/128"},
				"site1-follower-1": {"172.16.0.4/32", "2001:db8::4/128"},
				"site1-follower-2": {"172.16.0.5/32", "2001:db8::5/128"},
				"site1-follower-3": {"172.16.0.6/32", "2001:db8::6/128"},
				"site2-follower-1": {"172.16.0.7/32", "2001:db8::7/128"},
				"site2-follower-2": {"172.16.0.8/32", "2001:db8::8/128"},
				"site2-follower-3": {"172.16.0.9/32", "2001:db8::9/128"},
				"site3-follower-1": {"172.16.0.10/32", "2001:db8::10/128"},
				"site3-follower-2": {"172.16.0.11/32", "2001:db8::11/128"},
				"site3-follower-3": {"172.16.0.12/32", "2001:db8::12/128"},
			},
			edges: map[string][]string{
				"site1-router": {"site2-router", "site3-router", "site1-follower-1", "site1-follower-2", "site1-follower-3"},
				"site2-router": {"site1-router", "site3-router", "site2-follower-1", "site2-follower-2", "site2-follower-3"},
				"site3-router": {"site1-router", "site2-router", "site3-follower-1", "site3-follower-2", "site3-follower-3"},
			},
			wantIPs: map[string]map[string][]string{
				"site1-router": {
					"site2-router": {
						// Site 2 is reachable via site 2 router
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
					},
					"site3-router": {
						// Site 3 is reachable via site 3 router
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
					"site1-follower-1": {"172.16.0.4/32", "2001:db8::4/128"},
					"site1-follower-2": {"172.16.0.5/32", "2001:db8::5/128"},
					"site1-follower-3": {"172.16.0.6/32", "2001:db8::6/128"},
				},
				"site2-router": {
					"site1-router": {
						// Site 1 is reachable via site 1 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
					"site3-router": {
						// Site 3 is reachable via site 3 router
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
					"site2-follower-1": {"172.16.0.7/32", "2001:db8::7/128"},
					"site2-follower-2": {"172.16.0.8/32", "2001:db8::8/128"},
					"site2-follower-3": {"172.16.0.9/32", "2001:db8::9/128"},
				},
				"site3-router": {
					"site1-router": {
						// Site 1 is reachable via site 1 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
					},
					"site2-router": {
						// Site 2 is reachable via site 2 router
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
					},
					"site3-follower-1": {"172.16.0.10/32", "2001:db8::10/128"},
					"site3-follower-2": {"172.16.0.11/32", "2001:db8::11/128"},
					"site3-follower-3": {"172.16.0.12/32", "2001:db8::12/128"},
				},
				"site1-follower-1": {
					"site1-router": {
						// Everyone is reachable via site 1 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site1-follower-2": {
					"site1-router": {
						// Everyone is reachable via site 1 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site1-follower-3": {
					"site1-router": {
						// Everyone is reachable via site 1 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site2-follower-1": {
					"site2-router": {
						// Everyone is reachable via site 2 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site2-follower-2": {
					"site2-router": {
						// Everyone is reachable via site 2 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site2-follower-3": {
					"site2-router": {
						// Everyone is reachable via site 2 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site3-follower-1": {
					"site3-router": {
						// Everyone is reachable via site 3 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.11/32", "2001:db8::11/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site3-follower-2": {
					"site3-router": {
						// Everyone is reachable via site 3 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.12/32", "2001:db8::12/128",
					},
				},
				"site3-follower-3": {
					"site3-router": {
						// Everyone is reachable via site 3 router
						"172.16.0.1/32", "2001:db8::1/128",
						"172.16.0.2/32", "2001:db8::2/128",
						"172.16.0.3/32", "2001:db8::3/128",
						"172.16.0.4/32", "2001:db8::4/128",
						"172.16.0.5/32", "2001:db8::5/128",
						"172.16.0.6/32", "2001:db8::6/128",
						"172.16.0.7/32", "2001:db8::7/128",
						"172.16.0.8/32", "2001:db8::8/128",
						"172.16.0.9/32", "2001:db8::9/128",
						"172.16.0.10/32", "2001:db8::10/128",
						"172.16.0.11/32", "2001:db8::11/128",
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.NewTestStorage()
			if err != nil {
				t.Fatalf("create test db: %v", err)
			}
			defer db.Close()
			peerdb := peers.New(db)
			nw := networking.New(db)
			// Create an allow-all traffic policy.
			err = nw.PutNetworkACL(ctx, &v1.NetworkACL{
				Name:             "allow-all",
				Action:           v1.ACLAction_ACTION_ACCEPT,
				SourceNodes:      []string{"*"},
				DestinationNodes: []string{"*"},
				SourceCidrs:      []string{"*"},
				DestinationCidrs: []string{"*"},
			})
			if err != nil {
				t.Fatalf("put network acl: %v", err)
			}
			for peerID, addrs := range tc.peers {
				_, err = peerdb.Put(ctx, &peers.PutOptions{
					ID:        peerID,
					PublicKey: mustGenerateKey(t).PublicKey(),
				})
				if err != nil {
					t.Fatalf("put peer %q: %v", peerID, err)
				}
				err = peerdb.PutLease(ctx, &peers.PutLeaseOptions{
					ID:   peerID,
					IPv4: netip.MustParsePrefix(addrs[0]),
					IPv6: netip.MustParsePrefix(addrs[1]),
				})
				if err != nil {
					t.Fatalf("put lease for peer %q: %v", peerID, err)
				}
			}
			for peerID, edges := range tc.edges {
				for _, edge := range edges {
					err = peerdb.PutEdge(ctx, peers.Edge{
						From: peerID,
						To:   edge,
					})
					if err != nil {
						t.Fatalf("put edge from %q to %q: %v", peerID, edge, err)
					}
				}
			}
			for peer, want := range tc.wantIPs {
				peers, err := WireGuardPeersFor(ctx, db, peer)
				if err != nil {
					t.Fatalf("get peers for %q: %v", peer, err)
				}
				// Make sure strings are sorted for comparison.
				for _, ips := range want {
					sort.Strings(ips)
				}
				got := make(map[string][]string)
				for _, p := range peers {
					sort.Strings(p.AllowedIps)
					got[p.Id] = p.AllowedIps
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("peer: %s got %v, want %v", peer, got, want)
				}
			}
		})
	}
}

func mustGenerateKey(t *testing.T) wgtypes.Key {
	t.Helper()
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}
