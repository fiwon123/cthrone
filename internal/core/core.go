package core

import (
	"context"
	"fmt"
	"log"

	"github.com/fiwon123/cthrone/internal/handlers/chat"
	"github.com/fiwon123/cthrone/internal/servers/ws"
	"github.com/grandcat/zeroconf"
)

const (
	serviceType = "_cthrone._tcp"
	domain      = "local."
	servicePort = 8080
)

func Init(name string) {

	if name == "" {
		fmt.Println("name is empty")
		return
	}

	go ws.StartServer(servicePort)

	log.Println("resgistering server...")
	server, err := zeroconf.Register(name, serviceType, domain, servicePort, nil, nil)
	if err != nil {
		log.Fatal("mDNS registration failed:", err)
	}
	defer server.Shutdown()

	go browseDevices(name)

	select {}
}

func browseDevices(me string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatal("Failed to initialize resolver:", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go findConnectionLoop(entries, me)

	ctx := context.Background()
	if err := resolver.Browse(ctx, serviceType, domain, entries); err != nil {
		log.Fatal("Failed to browse:", err)
	}

	select {}
}

func findConnectionLoop(results <-chan *zeroconf.ServiceEntry, me string) {
	for entry := range results {

		if entry.Instance == me {
			continue
		}

		fmt.Println("Found device:", entry.Instance, entry.AddrIPv4, entry.Port)

		for _, ip := range entry.AddrIPv4 {
			url := fmt.Sprintf("ws://%s:%d/ws", ip, entry.Port)
			go chat.Connect(url)
		}
	}
}
