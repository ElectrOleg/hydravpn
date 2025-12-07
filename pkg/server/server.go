package server

import (
	"context"
	"crypto/rand"
	"encoding/binary"
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

// Server represents a HydraVPN server
type Server struct {
	config     *Config
	transport  transport.Transport
	listener   transport.Listener
	keyPair    *crypto.KeyPair
	tunDevice  *tun.TUNDevice
	
	sessions   map[uint64]*ClientSession
	sessionsMu sync.RWMutex
	
	ipPool     *IPPool
	
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// Config holds server configuration
type Config struct {
	ListenAddr    string
	TransportType transport.TransportType
	TUNConfig     *tun.Config
	EnableNAT     bool
}

// ClientSession represents a connected client
type ClientSession struct {
	ID           uint64
	Conn         transport.Connection
	CryptoSession *crypto.Session
	AssignedIP   net.IP
	LastSeen     time.Time
}

// IPPool manages IP address allocation for clients
type IPPool struct {
	baseIP   net.IP
	subnet   *net.IPNet
	used     map[string]bool
	mu       sync.Mutex
	nextHost uint32
}

// NewIPPool creates a new IP pool
func NewIPPool(subnet *net.IPNet) *IPPool {
	return &IPPool{
		baseIP:   subnet.IP,
		subnet:   subnet,
		used:     make(map[string]bool),
		nextHost: 2, // Start from .2 (reserve .1 for server)
	}
}

// Allocate allocates a new IP from the pool
func (p *IPPool) Allocate() (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Calculate available IPs in subnet
	ones, bits := p.subnet.Mask.Size()
	maxHosts := uint32(1 << (bits - ones))
	
	for i := uint32(0); i < maxHosts-2; i++ {
		hostNum := (p.nextHost + i) % (maxHosts - 1)
		if hostNum < 2 {
			hostNum = 2
		}
		
		ip := make(net.IP, 4)
		baseInt := binary.BigEndian.Uint32(p.baseIP.To4())
		binary.BigEndian.PutUint32(ip, baseInt+hostNum)
		
		ipStr := ip.String()
		if !p.used[ipStr] {
			p.used[ipStr] = true
			p.nextHost = hostNum + 1
			return ip, nil
		}
	}
	
	return nil, fmt.Errorf("IP pool exhausted")
}

// Release releases an IP back to the pool
func (p *IPPool) Release(ip net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, ip.String())
}

// DefaultConfig returns default server configuration
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:    ":8443",
		TransportType: transport.TransportWebSocket, // WebSocket for easier testing
		TUNConfig:     tun.DefaultConfig(),
		EnableNAT:     true,
	}
}

// New creates a new VPN server
func New(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	// Generate server key pair
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
	
	// Create IP pool
	_, subnet, _ := net.ParseCIDR("10.8.0.0/24")
	ipPool := NewIPPool(subnet)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Server{
		config:   cfg,
		transport: t,
		keyPair:  keyPair,
		sessions: make(map[uint64]*ClientSession),
		ipPool:   ipPool,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start starts the VPN server
func (s *Server) Start() error {
	log.Printf("Starting HydraVPN server on %s using %s transport",
		s.config.ListenAddr, s.transport.Name())
	
	// Create TUN device for server
	s.config.TUNConfig.LocalIP = net.ParseIP("10.8.0.1")
	tunDev, err := tun.New(s.config.TUNConfig)
	if err != nil {
		log.Printf("Warning: Failed to create TUN device: %v", err)
		log.Printf("Running in tunnel-only mode (no system routing)")
	} else {
		s.tunDevice = tunDev
		log.Printf("Created TUN interface: %s", tunDev.Name())
		
		// Start reading from TUN
		s.wg.Add(1)
		go s.tunReadLoop()
	}
	
	// Start listener
	listener, err := s.transport.Listen(s.ctx, s.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	s.listener = listener
	
	log.Printf("Server listening on %s", listener.Addr())
	
	// Accept connections
	s.wg.Add(1)
	go s.acceptLoop()
	
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		
		conn, err := s.listener.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			log.Printf("Accept error: %v", err)
			continue
		}
		
		log.Printf("New connection from %s", conn.RemoteAddr())
		
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single client connection
func (s *Server) handleConnection(conn transport.Connection) {
	defer s.wg.Done()
	defer conn.Close()
	
	// Wait for handshake init
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Read handshake error: %v", err)
		return
	}
	
	// Parse packet
	packet, err := protocol.UnmarshalPacket(buf[:n])
	if err != nil {
		log.Printf("Parse packet error: %v", err)
		return
	}
	
	if packet.Header.Type != protocol.PacketTypeHandshakeInit {
		log.Printf("Expected handshake init, got %d", packet.Header.Type)
		return
	}
	
	// Parse handshake init
	hsInit, err := protocol.UnmarshalHandshakeInit(packet.Payload)
	if err != nil {
		log.Printf("Parse handshake init error: %v", err)
		return
	}
	
	// Compute shared secret
	sharedSecret, err := crypto.ComputeSharedSecret(s.keyPair.PrivateKey, hsInit.ClientPublicKey)
	if err != nil {
		log.Printf("Compute shared secret error: %v", err)
		return
	}
	
	// Derive session keys
	salt, _ := crypto.GenerateRandomBytes(32)
	cryptoSession, err := crypto.DeriveSessionKeys(sharedSecret, false, salt)
	if err != nil {
		log.Printf("Derive keys error: %v", err)
		return
	}
	
	// Generate session ID
	var sessionID uint64
	binary.Read(rand.Reader, binary.BigEndian, &sessionID)
	
	// Allocate IP for client
	clientIP, err := s.ipPool.Allocate()
	if err != nil {
		log.Printf("IP allocation error: %v", err)
		return
	}
	
	// Create session
	session := &ClientSession{
		ID:            sessionID,
		Conn:          conn,
		CryptoSession: cryptoSession,
		AssignedIP:    clientIP,
		LastSeen:      time.Now(),
	}
	
	s.sessionsMu.Lock()
	s.sessions[sessionID] = session
	s.sessionsMu.Unlock()
	
	defer func() {
		s.sessionsMu.Lock()
		delete(s.sessions, sessionID)
		s.sessionsMu.Unlock()
		s.ipPool.Release(clientIP)
		log.Printf("Session %d closed, released IP %s", sessionID, clientIP)
	}()
	
	// Send handshake response
	hsResp := &protocol.HandshakeResponse{
		ServerPublicKey: s.keyPair.PublicKey,
		SessionID:       sessionID,
		Subnet:          24,
	}
	copy(hsResp.AssignedIP[:], clientIP.To4())
	copy(hsResp.ServerIP[:], net.ParseIP("10.8.0.1").To4())
	rand.Read(hsResp.RandomPadding[:])
	
	respPayload := protocol.MarshalHandshakeResponse(hsResp)
	respPacket := protocol.NewPacket(protocol.PacketTypeHandshakeResponse, sessionID, respPayload)
	
	if _, err := conn.Write(respPacket.Marshal()); err != nil {
		log.Printf("Write handshake response error: %v", err)
		return
	}
	
	log.Printf("Session %d established, assigned IP %s", sessionID, clientIP)
	
	// Handle data packets
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		
		n, err := conn.Read(buf)
		if err != nil {
			log.Printf("Session %d read error: %v", sessionID, err)
			return
		}
		
		packet, err := protocol.UnmarshalPacket(buf[:n])
		if err != nil {
			log.Printf("Session %d parse error: %v", sessionID, err)
			continue
		}
		
		session.LastSeen = time.Now()
		
		switch packet.Header.Type {
		case protocol.PacketTypeData:
			// Decrypt payload
			plaintext, err := cryptoSession.Decrypt(packet.Payload)
			if err != nil {
				log.Printf("Session %d decrypt error: %v", sessionID, err)
				continue
			}
			
			// Write to TUN device
			if s.tunDevice != nil {
				if _, err := s.tunDevice.Write(plaintext); err != nil {
					log.Printf("TUN write error: %v", err)
				}
			}
			
		case protocol.PacketTypeKeepAlive:
			// Send keepalive response
			kaPacket := protocol.NewPacket(protocol.PacketTypeKeepAlive, sessionID, nil)
			conn.Write(kaPacket.Marshal())
			
		case protocol.PacketTypeDisconnect:
			log.Printf("Session %d disconnected by client", sessionID)
			return
		}
	}
}

// tunReadLoop reads packets from TUN and sends to clients
func (s *Server) tunReadLoop() {
	defer s.wg.Done()
	
	buf := make([]byte, 2048)
	
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		
		n, err := s.tunDevice.Read(buf)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			log.Printf("TUN read error: %v", err)
			continue
		}
		
		// Parse IP header to find destination
		if n < 20 {
			continue
		}
		
		// IPv4 destination is at bytes 16-19
		destIP := net.IP(buf[16:20])
		
		// Find session with matching IP
		s.sessionsMu.RLock()
		for _, session := range s.sessions {
			if session.AssignedIP.Equal(destIP) {
				// Encrypt and send
				ciphertext, err := session.CryptoSession.Encrypt(buf[:n])
				if err != nil {
					log.Printf("Encrypt error: %v", err)
					continue
				}
				
				packet := protocol.NewPacket(protocol.PacketTypeData, session.ID, ciphertext)
				session.Conn.Write(packet.Marshal())
				break
			}
		}
		s.sessionsMu.RUnlock()
	}
}

// Stop stops the server
func (s *Server) Stop() error {
	log.Println("Stopping server...")
	s.cancel()
	
	if s.listener != nil {
		s.listener.Close()
	}
	
	if s.tunDevice != nil {
		s.tunDevice.Close()
	}
	
	s.wg.Wait()
	log.Println("Server stopped")
	return nil
}
