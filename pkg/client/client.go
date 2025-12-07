package client

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/hydravpn/hydra/pkg/crypto"
	"github.com/hydravpn/hydra/pkg/protocol"
	"github.com/hydravpn/hydra/pkg/transport"
	"github.com/hydravpn/hydra/pkg/tun"
)

// Client represents a HydraVPN client
type Client struct {
	config        *Config
	transport     transport.Transport
	conn          transport.Connection
	keyPair       *crypto.KeyPair
	cryptoSession *crypto.Session
	tunDevice     *tun.TUNDevice
	
	sessionID     uint64
	assignedIP    net.IP
	serverIP      net.IP
	
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	
	connected     bool
	connMu        sync.RWMutex
}

// Config holds client configuration
type Config struct {
	ServerAddr    string
	TransportType transport.TransportType
	AutoReconnect bool
	ReconnectDelay time.Duration
}

// DefaultConfig returns default client configuration
func DefaultConfig() *Config {
	return &Config{
		ServerAddr:     "127.0.0.1:8443",
		TransportType:  transport.TransportWebSocket,
		AutoReconnect:  true,
		ReconnectDelay: 5 * time.Second,
	}
}

// New creates a new VPN client
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	// Generate client key pair
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}
	
	// Create transport
	var t transport.Transport
	switch cfg.TransportType {
	case transport.TransportQUIC:
		t = transport.NewQUICTransport(nil)
	case transport.TransportWebSocket:
		t = transport.NewWebSocketTransport(nil)
	case transport.TransportObfuscated:
		t = transport.NewObfuscatedTransport(nil)
	default:
		t = transport.NewWebSocketTransport(nil)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Client{
		config:    cfg,
		transport: t,
		keyPair:   keyPair,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Connect connects to the VPN server
func (c *Client) Connect() error {
	log.Printf("Connecting to %s using %s transport...", 
		c.config.ServerAddr, c.transport.Name())
	
	// Dial server
	conn, err := c.transport.Dial(c.ctx, c.config.ServerAddr)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn
	
	log.Printf("Connected, performing handshake...")
	
	// Perform handshake
	if err := c.performHandshake(); err != nil {
		conn.Close()
		return fmt.Errorf("handshake failed: %w", err)
	}
	
	log.Printf("Handshake complete, session ID: %d", c.sessionID)
	log.Printf("Assigned VPN IP: %s, Server IP: %s", c.assignedIP, c.serverIP)
	
	// Create TUN device with assigned IP
	tunConfig := &tun.Config{
		Name:     "hydra0",
		MTU:      1400,
		LocalIP:  c.assignedIP,
		RemoteIP: c.serverIP,
	}
	_, tunConfig.Subnet, _ = net.ParseCIDR("10.8.0.0/24")
	
	tunDev, err := tun.New(tunConfig)
	if err != nil {
		log.Printf("Warning: Failed to create TUN device: %v", err)
		log.Printf("Running in tunnel-only mode (no system routing)")
	} else {
		c.tunDevice = tunDev
		log.Printf("Created TUN interface: %s", tunDev.Name())
		
		// Start reading from TUN
		c.wg.Add(1)
		go c.tunReadLoop()
	}
	
	c.connMu.Lock()
	c.connected = true
	c.connMu.Unlock()
	
	// Start receiving from server
	c.wg.Add(1)
	go c.receiveLoop()
	
	// Start keepalive
	c.wg.Add(1)
	go c.keepaliveLoop()
	
	log.Println("VPN tunnel established successfully!")
	
	return nil
}

// performHandshake performs the cryptographic handshake
func (c *Client) performHandshake() error {
	// Create handshake init
	hsInit := &protocol.HandshakeInit{
		ClientPublicKey: c.keyPair.PublicKey,
		Timestamp:       time.Now().Unix(),
	}
	rand.Read(hsInit.RandomPadding[:])
	
	// Send handshake init
	initPayload := protocol.MarshalHandshakeInit(hsInit)
	initPacket := protocol.NewPacket(protocol.PacketTypeHandshakeInit, 0, initPayload)
	
	if _, err := c.conn.Write(initPacket.Marshal()); err != nil {
		return fmt.Errorf("failed to send handshake init: %w", err)
	}
	
	// Receive handshake response
	buf := make([]byte, 4096)
	n, err := c.conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read handshake response: %w", err)
	}
	
	// Parse response packet
	respPacket, err := protocol.UnmarshalPacket(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	
	if respPacket.Header.Type != protocol.PacketTypeHandshakeResponse {
		return fmt.Errorf("unexpected packet type: %d", respPacket.Header.Type)
	}
	
	// Parse handshake response
	hsResp, err := protocol.UnmarshalHandshakeResponse(respPacket.Payload)
	if err != nil {
		return fmt.Errorf("failed to parse handshake response: %w", err)
	}
	
	// Compute shared secret
	sharedSecret, err := crypto.ComputeSharedSecret(c.keyPair.PrivateKey, hsResp.ServerPublicKey)
	if err != nil {
		return fmt.Errorf("failed to compute shared secret: %w", err)
	}
	
	// Derive session keys (client is initiator)
	salt := make([]byte, 32) // In real implementation, exchange salt
	c.cryptoSession, err = crypto.DeriveSessionKeys(sharedSecret, true, salt)
	if err != nil {
		return fmt.Errorf("failed to derive keys: %w", err)
	}
	
	// Store session info
	c.sessionID = hsResp.SessionID
	c.assignedIP = net.IP(hsResp.AssignedIP[:])
	c.serverIP = net.IP(hsResp.ServerIP[:])
	
	return nil
}

// tunReadLoop reads from TUN and sends to server
func (c *Client) tunReadLoop() {
	defer c.wg.Done()
	
	buf := make([]byte, 2048)
	
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		
		n, err := c.tunDevice.Read(buf)
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("TUN read error: %v", err)
			continue
		}
		
		// Encrypt data
		ciphertext, err := c.cryptoSession.Encrypt(buf[:n])
		if err != nil {
			log.Printf("Encrypt error: %v", err)
			continue
		}
		
		// Send to server
		packet := protocol.NewPacket(protocol.PacketTypeData, c.sessionID, ciphertext)
		if _, err := c.conn.Write(packet.Marshal()); err != nil {
			log.Printf("Send error: %v", err)
			c.handleDisconnect()
			return
		}
	}
}

// receiveLoop receives packets from server
func (c *Client) receiveLoop() {
	defer c.wg.Done()
	
	buf := make([]byte, 4096)
	
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		
		n, err := c.conn.Read(buf)
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("Receive error: %v", err)
			c.handleDisconnect()
			return
		}
		
		packet, err := protocol.UnmarshalPacket(buf[:n])
		if err != nil {
			log.Printf("Parse error: %v", err)
			continue
		}
		
		switch packet.Header.Type {
		case protocol.PacketTypeData:
			// Decrypt and write to TUN
			plaintext, err := c.cryptoSession.Decrypt(packet.Payload)
			if err != nil {
				log.Printf("Decrypt error: %v", err)
				continue
			}
			
			if c.tunDevice != nil {
				if _, err := c.tunDevice.Write(plaintext); err != nil {
					log.Printf("TUN write error: %v", err)
				}
			}
			
		case protocol.PacketTypeKeepAlive:
			// Server acknowledged keepalive
			
		case protocol.PacketTypeDisconnect:
			log.Println("Server disconnected")
			c.handleDisconnect()
			return
		}
	}
}

// keepaliveLoop sends periodic keepalive packets
func (c *Client) keepaliveLoop() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(protocol.KeepAliveInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.connMu.RLock()
			if !c.connected {
				c.connMu.RUnlock()
				return
			}
			c.connMu.RUnlock()
			
			packet := protocol.NewPacket(protocol.PacketTypeKeepAlive, c.sessionID, nil)
			if _, err := c.conn.Write(packet.Marshal()); err != nil {
				log.Printf("Keepalive error: %v", err)
			}
		}
	}
}

// handleDisconnect handles connection loss
func (c *Client) handleDisconnect() {
	c.connMu.Lock()
	if !c.connected {
		c.connMu.Unlock()
		return
	}
	c.connected = false
	c.connMu.Unlock()
	
	log.Println("Disconnected from server")
	
	if c.config.AutoReconnect {
		go c.reconnectLoop()
	}
}

// reconnectLoop attempts to reconnect
func (c *Client) reconnectLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		
		log.Printf("Attempting to reconnect in %v...", c.config.ReconnectDelay)
		time.Sleep(c.config.ReconnectDelay)
		
		if err := c.Connect(); err != nil {
			log.Printf("Reconnect failed: %v", err)
			continue
		}
		
		return
	}
}

// Disconnect disconnects from the server
func (c *Client) Disconnect() error {
	log.Println("Disconnecting...")
	
	c.connMu.Lock()
	c.connected = false
	c.connMu.Unlock()
	
	// Send disconnect packet
	if c.conn != nil {
		packet := protocol.NewPacket(protocol.PacketTypeDisconnect, c.sessionID, nil)
		c.conn.Write(packet.Marshal())
		c.conn.Close()
	}
	
	c.cancel()
	
	if c.tunDevice != nil {
		c.tunDevice.Close()
	}
	
	c.wg.Wait()
	log.Println("Disconnected")
	return nil
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.connected
}

// AssignedIP returns the assigned VPN IP
func (c *Client) AssignedIP() net.IP {
	return c.assignedIP
}
