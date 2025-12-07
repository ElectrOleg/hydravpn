package transport

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// ObfuscatedTransport implements Transport with traffic obfuscation
// Traffic looks like regular TLS/HTTPS to deep packet inspection
type ObfuscatedTransport struct {
	tlsConfig *tls.Config
	key       []byte // XOR key for additional obfuscation
}

// ObfuscatedConnection wraps a TLS connection with obfuscation
type ObfuscatedConnection struct {
	conn   net.Conn
	key    []byte
	keyPos int
}

// ObfuscatedListener wraps a TLS listener
type ObfuscatedListener struct {
	listener net.Listener
	key      []byte
}

// NewObfuscatedTransport creates a new obfuscated transport
func NewObfuscatedTransport(tlsConfig *tls.Config) *ObfuscatedTransport {
	// Generate random XOR key
	key := make([]byte, 32)
	rand.Read(key)
	
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	
	return &ObfuscatedTransport{
		tlsConfig: tlsConfig,
		key:       key,
	}
}

// SetKey sets the obfuscation key (must match on client and server)
func (t *ObfuscatedTransport) SetKey(key []byte) {
	t.key = key
}

// Name returns the transport name
func (t *ObfuscatedTransport) Name() string {
	return "obfuscated"
}

// Dial connects with obfuscation
func (t *ObfuscatedTransport) Dial(ctx context.Context, address string) (Connection, error) {
	// Use standard TLS dialer
	dialer := &tls.Dialer{
		Config: t.tlsConfig,
	}
	
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("obfuscated dial failed: %w", err)
	}
	
	return &ObfuscatedConnection{
		conn: conn,
		key:  t.key,
	}, nil
}

// Listen starts an obfuscated listener
func (t *ObfuscatedTransport) Listen(ctx context.Context, address string) (Listener, error) {
	listener, err := tls.Listen("tcp", address, t.tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("obfuscated listen failed: %w", err)
	}
	
	return &ObfuscatedListener{
		listener: listener,
		key:      t.key,
	}, nil
}

// Close closes the transport
func (t *ObfuscatedTransport) Close() error {
	return nil
}

// obfuscate applies XOR obfuscation to data
func obfuscate(data []byte, key []byte, keyPos *int) {
	for i := range data {
		data[i] ^= key[*keyPos]
		*keyPos = (*keyPos + 1) % len(key)
	}
}

// Read reads and de-obfuscates data
func (c *ObfuscatedConnection) Read(b []byte) (n int, err error) {
	// Read length prefix (4 bytes)
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, lenBuf); err != nil {
		return 0, err
	}
	
	// De-obfuscate length
	tempKeyPos := c.keyPos
	obfuscate(lenBuf, c.key, &tempKeyPos)
	
	length := binary.BigEndian.Uint32(lenBuf)
	if length > 65535 {
		return 0, fmt.Errorf("invalid packet length: %d", length)
	}
	
	// Read actual data
	data := make([]byte, length)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return 0, err
	}
	
	// De-obfuscate data
	obfuscate(data, c.key, &c.keyPos)
	
	n = copy(b, data)
	return n, nil
}

// Write obfuscates and writes data
func (c *ObfuscatedConnection) Write(b []byte) (n int, err error) {
	// Create packet with length prefix
	packet := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(packet[:4], uint32(len(b)))
	copy(packet[4:], b)
	
	// Obfuscate
	tempKeyPos := c.keyPos
	obfuscate(packet, c.key, &tempKeyPos)
	c.keyPos = tempKeyPos
	
	// Write
	_, err = c.conn.Write(packet)
	if err != nil {
		return 0, err
	}
	
	return len(b), nil
}

// Close closes the connection
func (c *ObfuscatedConnection) Close() error {
	return c.conn.Close()
}

// LocalAddr returns the local address
func (c *ObfuscatedConnection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address
func (c *ObfuscatedConnection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// Accept accepts a new obfuscated connection
func (l *ObfuscatedListener) Accept() (Connection, error) {
	conn, err := l.listener.Accept()
	if err != nil {
		return nil, err
	}
	
	return &ObfuscatedConnection{
		conn: conn,
		key:  l.key,
	}, nil
}

// Close closes the listener
func (l *ObfuscatedListener) Close() error {
	return l.listener.Close()
}

// Addr returns the listener address
func (l *ObfuscatedListener) Addr() net.Addr {
	return l.listener.Addr()
}
