//go:build !wasm

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

package libp2p

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/protobuf/proto"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/net/transport"
)

// JoinAnnounceOptions are options for announcing the host or discovering peers
// on the libp2p kademlia DHT.
type JoinAnnounceOptions struct {
	// PSK is the pre-shared key to use as a rendezvous point for the DHT.
	PSK string
	// BootstrapPeers is a list of bootstrap peers to use for the DHT.
	// If empty or nil, the default bootstrap peers will be used.
	BootstrapPeers []multiaddr.Multiaddr
	// Options are options for configuring the libp2p host.
	Options []libp2p.Option
	// AnnounceTTL is the TTL to use for the discovery service.
	AnnounceTTL time.Duration
	// LocalAddrs is a list of local addresses to announce the host with.
	// If empty or nil, the default local addresses will be used.
	LocalAddrs []multiaddr.Multiaddr
	// ConnectTimeout is the timeout to use when connecting to peers.
	ConnectTimeout time.Duration
}

// NewJoinAnnouncer creates a new announcer on the kadmilia DHT and executes
// received join requests against the given join Server.
func NewJoinAnnouncer(ctx context.Context, opts JoinAnnounceOptions, join transport.JoinServer) (io.Closer, error) {
	log := context.LoggerFrom(ctx)
	SetBuffers(ctx)
	acceptc := make(chan network.Stream, 1)
	if len(opts.LocalAddrs) > 0 {
		opts.Options = append(opts.Options, libp2p.ListenAddrs(opts.LocalAddrs...))
	}
	host, err := libp2p.New(opts.Options...)
	if err != nil {
		return nil, fmt.Errorf("libp2p new host: %w", err)
	}
	host.SetStreamHandler(JoinProtocol, func(s network.Stream) {
		log.Debug("Handling join protocol stream", "peer", s.Conn().RemotePeer())
		acceptc <- s
	})
	log = log.With(slog.String("host-id", host.ID().String()))
	ctx = context.WithLogger(ctx, log)
	// Bootstrap the DHT.
	log.Debug("Bootstrapping DHT")
	kaddht, err := NewDHT(ctx, host, opts.BootstrapPeers, opts.ConnectTimeout)
	if err != nil {
		defer host.Close()
		return nil, fmt.Errorf("libp2p new dht: %w", err)
	}
	// Announce the join protocol with our PSK.
	log.Debug("Announcing join protocol with our PSK")
	routingDiscovery := drouting.NewRoutingDiscovery(kaddht)
	var discoveryOpts []discovery.Option
	if opts.AnnounceTTL > 0 {
		discoveryOpts = append(discoveryOpts, discovery.TTL(opts.AnnounceTTL))
	}
	dutil.Advertise(context.Background(), routingDiscovery, opts.PSK, discoveryOpts...)
	announcer := &dhtJoinAnnouncer{
		JoinAnnounceOptions: opts,
		host:                host,
		dht:                 kaddht,
		acceptc:             acceptc,
		closec:              make(chan struct{}),
	}
	go announcer.handleIncomingStreams(log, join)
	return announcer, nil
}

type dhtJoinAnnouncer struct {
	JoinAnnounceOptions
	host    host.Host
	dht     *dht.IpfsDHT
	acceptc chan network.Stream
	closec  chan struct{}
}

func (srv *dhtJoinAnnouncer) handleIncomingStreams(log *slog.Logger, joinServer transport.JoinServer) {
	returnErr := func(stream network.Stream, err error) {
		log.Error("Failed to handle join protocol stream", slog.String("error", err.Error()))
		buf := []byte("ERROR: " + err.Error())
		if _, err := stream.Write(buf); err != nil {
			log.Error("Failed to write error to peer", slog.String("error", err.Error()))
		}
	}
	for {
		select {
		case <-srv.closec:
			return
		case conn := <-srv.acceptc:
			go func() {
				rlog := log.With(slog.String("peer-id", conn.Conn().RemotePeer().String()))
				rlog.Debug("Handling join protocol stream")
				defer conn.Close()
				// Read a join request off the wire
				var b [8192]byte
				n, err := conn.Read(b[:])
				if err != nil {
					rlog.Error("Failed to read join request from peer", slog.String("error", err.Error()))
					returnErr(conn, err)
					return
				}
				buf := b[:n]
				var req v1.JoinRequest
				err = proto.Unmarshal(buf, &req)
				if err != nil {
					rlog.Error("Failed to unmarshal join request from peer", slog.String("error", err.Error()))
					returnErr(conn, err)
					return
				}
				// Execute the join request
				rlog.Debug("Executing join request")
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*15) // TODO: Make this configurable
				defer cancel()
				resp, err := joinServer.Serve(context.WithLogger(ctx, rlog), &req)
				if err != nil {
					rlog.Error("Failed to execute join request", slog.String("error", err.Error()))
					returnErr(conn, err)
					return
				}
				// Write the response back to the peer
				buf, err = proto.Marshal(resp)
				if err != nil {
					rlog.Error("Failed to marshal join response", slog.String("error", err.Error()))
					returnErr(conn, err)
					return
				}
				if _, err := conn.Write(buf); err != nil {
					rlog.Error("Failed to write join response to peer", slog.String("error", err.Error()))
					return
				}
			}()
		}
	}
}

func (srv *dhtJoinAnnouncer) Close() error {
	defer close(srv.closec)
	defer srv.host.Close()
	return srv.dht.Close()
}