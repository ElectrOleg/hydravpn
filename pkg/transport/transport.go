package transport

import (
	"context"
	"net"
)

// Transport defines the interface for different transport methods
type Transport interface {
	// Name returns the transport name
	Name() string
	
	// Dial connects to a remote address
	Dial(ctx context.Context, address string) (Connection, error)
	
	// Listen starts listening for incoming connections
	Listen(ctx context.Context, address string) (Listener, error)
	
	// Close closes the transport
	Close() error
}

// Connection represents an established connection
type Connection interface {
	// Read reads data from the connection
	Read(b []byte) (n int, err error)
	
	// Write writes data to the connection
	Write(b []byte) (n int, err error)
	
	// Close closes the connection
	Close() error
	
	// LocalAddr returns the local address
	LocalAddr() net.Addr
	
	// RemoteAddr returns the remote address
	RemoteAddr() net.Addr
}

// Listener represents a transport listener
type Listener interface {
	// Accept accepts incoming connections
	Accept() (Connection, error)
	
	// Close closes the listener
	Close() error
	
	// Addr returns the listener's address
	Addr() net.Addr
}

// TransportType identifies the transport type
type TransportType int

const (
	TransportQUIC TransportType = iota
	TransportWebSocket
	TransportObfuscated
)

func (t TransportType) String() string {
	switch t {
	case TransportQUIC:
		return "quic"
	case TransportWebSocket:
		return "websocket"
	case TransportObfuscated:
		return "obfuscated"
	default:
		return "unknown"
	}
}
