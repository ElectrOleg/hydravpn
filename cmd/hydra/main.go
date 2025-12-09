package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hydravpn/hydra/pkg/client"
	"github.com/hydravpn/hydra/pkg/server"
	"github.com/hydravpn/hydra/pkg/transport"
)

const banner = `
╦ ╦╦ ╦╔╦╗╦═╗╔═╗  ╦  ╦╔═╗╔╗╔
╠═╣╚╦╝ ║║╠╦╝╠═╣  ╚╗╔╝╠═╝║║║
╩ ╩ ╩ ═╩╝╩╚═╩ ╩   ╚╝ ╩  ╝╚╝
  Next-Gen VPN Protocol
  
`

func main() {
	// Parse command
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	
	command := os.Args[1]
	
	switch command {
	case "server":
		runServer()
	case "client":
		runClient()
	case "version":
		fmt.Println("HydraVPN v0.1.0 (MVP)")
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(banner)
	fmt.Println("Usage: hydra <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  server    Start VPN server")
	fmt.Println("  client    Connect to VPN server")
	fmt.Println("  version   Show version")
	fmt.Println("  help      Show this help")
	fmt.Println()
	fmt.Println("Server options:")
	fmt.Println("  --listen <addr>     Listen address (default: :8443)")
	fmt.Println("  --transport <type>  Transport: websocket, quic, obfs (default: websocket)")
	fmt.Println()
	fmt.Println("Client options:")
	fmt.Println("  --server <addr>     Server address (default: 127.0.0.1:8443)")
	fmt.Println("  --transport <type>  Transport: websocket, quic, obfs (default: websocket)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sudo hydra server --listen :8443")
	fmt.Println("  sudo hydra client --server 192.168.1.100:8443")
}

func runServer() {
	fmt.Print(banner)
	
	serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
	listen := serverFlags.String("listen", ":8443", "Listen address")
	transportType := serverFlags.String("transport", "websocket", "Transport type")
	
	serverFlags.Parse(os.Args[2:])
	
	cfg := server.DefaultConfig()
	cfg.ListenAddr = *listen
	cfg.TransportType = parseTransport(*transportType)
	
	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	
	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	log.Println("Server running. Press Ctrl+C to stop.")
	<-sigChan
	
	srv.Stop()
}

func runClient() {
	fmt.Print(banner)

	clientFlags := flag.NewFlagSet("client", flag.ExitOnError)
	serverAddr := clientFlags.String("server", "127.0.0.1:8443", "Server address")
	transportType := clientFlags.String("transport", "websocket", "Transport type")

	clientFlags.Parse(os.Args[2:])

	cfg := client.DefaultConfig()
	cfg.ServerAddr = *serverAddr
	cfg.TransportType = parseTransport(*transportType)
	cfg.AutoReconnect = false // Disable auto-reconnect on manual disconnect

	cli, err := client.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Ensure cleanup happens even on panic
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic recovered: %v", r)
		}
		log.Println("Cleaning up...")
		cli.Disconnect()
	}()

	if err := cli.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Connected. Press Ctrl+C to disconnect.")
	<-sigChan

	// Disconnect is called in defer
}

func parseTransport(t string) transport.TransportType {
	switch t {
	case "quic":
		return transport.TransportQUIC
	case "websocket", "ws":
		return transport.TransportWebSocket
	case "obfuscated", "obfs":
		return transport.TransportObfuscated
	default:
		return transport.TransportWebSocket
	}
}
