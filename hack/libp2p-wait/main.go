package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	"github.com/webmeshproj/webmesh/pkg/util/logutil"
)

const JoinProtocol = protocol.ID("/webmesh/join/0.0.1")

var log *slog.Logger

func main() {
	psk := flag.String("psk", "", "Pre-shared key")
	logLevel := flag.String("log-level", "info", "Log level")
	flag.Parse()

	if *psk == "" {
		panic("psk is required")
	}

	ctx := context.Background()
	log = logutil.SetupLogging(*logLevel)
	host, err := libp2p.New()
	if err != nil {
		panic(err)
	}
	log.Info("libp2p host created", "id", host.ID(), "addrs", host.Addrs())
	host.SetStreamHandler(JoinProtocol, func(s network.Stream) {
		log.Info("Handling join protocol stream", "id", host.ID())
		rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))
		go readData(rw)
		go writeData(rw)
	})

	kad, err := dht.New(ctx, host)
	if err != nil {
		panic(err)
	}
	defer kad.Close()

	log.Info("libp2p dht created, bootstrapping", "id", kad.PeerID())
	err = kad.Bootstrap(ctx)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for _, peerAddr := range dht.DefaultBootstrapPeers {
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := host.Connect(ctx, *peerinfo); err != nil {
				log.Warn("failed to connect to bootstrap peer", "error", err.Error())
			} else {
				log.Info("Connection established with bootstrap node", "node", *peerinfo)
			}
		}()
	}
	wg.Wait()

	log.Info("Announcing join protocol", "id", host.ID())
	routingDiscovery := drouting.NewRoutingDiscovery(kad)
	dutil.Advertise(ctx, routingDiscovery, *psk, discovery.TTL(time.Minute))
	log.Info("Announced join protocol", "id", host.ID())

	log.Info("Waiting for peers to join")
	<-ctx.Done()
}

func readData(rw *bufio.ReadWriter) {
	for {
		str, err := rw.ReadString('\n')
		if err != nil {
			log.Error("Error reading from buffer", "error", err.Error())
			return
		}
		if str == "" {
			return
		}
		if str != "\n" {
			fmt.Printf("\x1b[32m%s\x1b[0m> ", str)
		}
	}
}

func writeData(rw *bufio.ReadWriter) {
	stdReader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		sendData, err := stdReader.ReadString('\n')
		if err != nil {
			log.Error("Error reading from stdin", "error", err.Error())
			return
		}
		_, err = rw.WriteString(fmt.Sprintf("%s\n", sendData))
		if err != nil {
			log.Error("Error writing to buffer", "error", err.Error())
			return
		}
		err = rw.Flush()
		if err != nil {
			log.Error("Error flushing buffer", "error", err.Error())
			return
		}
	}
}
