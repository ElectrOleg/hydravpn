package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/quic-go/quic-go"
)

// QUICTransport implements Transport using QUIC protocol
type QUICTransport struct {
	tlsConfig *tls.Config
	quicConfig *quic.Config
}

// QUICConnection wraps a QUIC stream as a Connection
type QUICConnection struct {
	stream quic.Stream
	conn   quic.Connection
}

// QUICListener wraps a QUIC listener
type QUICListener struct {
	listener *quic.Listener
}

// NewQUICTransport creates a new QUIC transport
func NewQUICTransport(tlsConfig *tls.Config) *QUICTransport {
	if tlsConfig == nil {
		// Generate self-signed certificate for testing
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"hydravpn"},
		}
	}
	
	return &QUICTransport{
		tlsConfig: tlsConfig,
		quicConfig: &quic.Config{
			MaxIdleTimeout:        30_000_000_000, // 30 seconds in nanoseconds
			KeepAlivePeriod:       10_000_000_000, // 10 seconds
			EnableDatagrams:       true,
		},
	}
}

// Name returns the transport name
func (t *QUICTransport) Name() string {
	return "quic"
}

// Dial connects to a remote QUIC server
func (t *QUICTransport) Dial(ctx context.Context, address string) (Connection, error) {
	conn, err := quic.DialAddr(ctx, address, t.tlsConfig, t.quicConfig)
	if err != nil {
		return nil, fmt.Errorf("quic dial failed: %w", err)
	}
	
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to open stream")
		return nil, fmt.Errorf("failed to open QUIC stream: %w", err)
	}
	
	return &QUICConnection{
		stream: stream,
		conn:   conn,
	}, nil
}

// Listen starts a QUIC listener
func (t *QUICTransport) Listen(ctx context.Context, address string) (Listener, error) {
	listener, err := quic.ListenAddr(address, t.tlsConfig, t.quicConfig)
	if err != nil {
		return nil, fmt.Errorf("quic listen failed: %w", err)
	}
	
	return &QUICListener{listener: listener}, nil
}

// Close closes the transport
func (t *QUICTransport) Close() error {
	return nil
}

// Read reads from the QUIC stream
func (c *QUICConnection) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

// Write writes to the QUIC stream
func (c *QUICConnection) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

// Close closes the QUIC connection
func (c *QUICConnection) Close() error {
	c.stream.Close()
	return c.conn.CloseWithError(0, "connection closed")
}

// LocalAddr returns the local address
func (c *QUICConnection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address
func (c *QUICConnection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// Accept accepts a new QUIC connection
func (l *QUICListener) Accept() (Connection, error) {
	ctx := context.Background()
	conn, err := l.listener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to accept stream")
		return nil, err
	}
	
	return &QUICConnection{
		stream: stream,
		conn:   conn,
	}, nil
}

// Close closes the listener
func (l *QUICListener) Close() error {
	return l.listener.Close()
}

// Addr returns the listener address
func (l *QUICListener) Addr() net.Addr {
	return l.listener.Addr()
}
