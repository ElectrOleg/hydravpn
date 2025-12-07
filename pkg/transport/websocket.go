package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// WebSocketTransport implements Transport using WebSocket
type WebSocketTransport struct {
	tlsConfig *tls.Config
	dialer    *websocket.Dialer
	upgrader  websocket.Upgrader
}

// WebSocketConnection wraps a WebSocket connection
type WebSocketConnection struct {
	conn      *websocket.Conn
	readBuf   []byte
	readMutex sync.Mutex
}

// WebSocketListener wraps an HTTP server for WebSocket
type WebSocketListener struct {
	server     *http.Server
	listener   net.Listener
	connChan   chan *WebSocketConnection
	closeChan  chan struct{}
	closeOnce  sync.Once
}

// NewWebSocketTransport creates a new WebSocket transport
func NewWebSocketTransport(tlsConfig *tls.Config) *WebSocketTransport {
	return &WebSocketTransport{
		tlsConfig: tlsConfig,
		dialer: &websocket.Dialer{
			TLSClientConfig: tlsConfig,
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for VPN
			},
		},
	}
}

// Name returns the transport name
func (t *WebSocketTransport) Name() string {
	return "websocket"
}

// Dial connects to a WebSocket server
func (t *WebSocketTransport) Dial(ctx context.Context, address string) (Connection, error) {
	url := fmt.Sprintf("wss://%s/hydra", address)
	
	conn, _, err := t.dialer.DialContext(ctx, url, nil)
	if err != nil {
		// Try without TLS for local testing
		url = fmt.Sprintf("ws://%s/hydra", address)
		conn, _, err = t.dialer.DialContext(ctx, url, nil)
		if err != nil {
			return nil, fmt.Errorf("websocket dial failed: %w", err)
		}
	}
	
	return &WebSocketConnection{
		conn: conn,
	}, nil
}

// Listen starts a WebSocket listener
func (t *WebSocketTransport) Listen(ctx context.Context, address string) (Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tcp listen failed: %w", err)
	}
	
	wsListener := &WebSocketListener{
		listener:  listener,
		connChan:  make(chan *WebSocketConnection, 100),
		closeChan: make(chan struct{}),
	}
	
	mux := http.NewServeMux()
	mux.HandleFunc("/hydra", func(w http.ResponseWriter, r *http.Request) {
		conn, err := t.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		
		select {
		case wsListener.connChan <- &WebSocketConnection{conn: conn}:
		case <-wsListener.closeChan:
			conn.Close()
		}
	})
	
	wsListener.server = &http.Server{
		Handler: mux,
	}
	
	go wsListener.server.Serve(listener)
	
	return wsListener, nil
}

// Close closes the transport
func (t *WebSocketTransport) Close() error {
	return nil
}

// Read reads from the WebSocket connection
func (c *WebSocketConnection) Read(b []byte) (n int, err error) {
	c.readMutex.Lock()
	defer c.readMutex.Unlock()
	
	// If we have buffered data, return it first
	if len(c.readBuf) > 0 {
		n = copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}
	
	// Read new message
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	
	n = copy(b, data)
	if n < len(data) {
		c.readBuf = data[n:]
	}
	
	return n, nil
}

// Write writes to the WebSocket connection
func (c *WebSocketConnection) Write(b []byte) (n int, err error) {
	err = c.conn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close closes the WebSocket connection
func (c *WebSocketConnection) Close() error {
	return c.conn.Close()
}

// LocalAddr returns the local address
func (c *WebSocketConnection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address
func (c *WebSocketConnection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// Accept accepts a new WebSocket connection
func (l *WebSocketListener) Accept() (Connection, error) {
	select {
	case conn := <-l.connChan:
		return conn, nil
	case <-l.closeChan:
		return nil, fmt.Errorf("listener closed")
	}
}

// Close closes the listener
func (l *WebSocketListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closeChan)
	})
	l.server.Close()
	return l.listener.Close()
}

// Addr returns the listener address
func (l *WebSocketListener) Addr() net.Addr {
	return l.listener.Addr()
}
