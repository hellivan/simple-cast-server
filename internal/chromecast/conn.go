package chromecast

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	defaultCastPort = 8009
	senderID        = "sender-0"
	platformID      = "receiver-0"

	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"

	dialTimeout      = 5 * time.Second
	heartbeatPeriod  = 5 * time.Second
	writeTimeout     = 5 * time.Second
	readIdleTimeout  = 15 * time.Second
	connectBacklog   = 8
	requestIDCounter = 1
)

// message is the minimal JSON envelope shared by all Cast V2 control
// messages. Concrete payloads embed this struct.
type message struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId,omitempty"`
}

// conn manages a single TLS connection to a Chromecast/Nest Hub device and
// implements the small subset of the Cast V2 protocol required to launch
// and control the DashCast receiver app.
type conn struct {
	host string
	port int
	log  *slog.Logger

	mu        sync.Mutex
	tlsConn   *tls.Conn
	requestID int
	connected bool

	openChannels map[string]bool

	// pending holds channels waiting for a reply with a given requestId.
	pending map[int]chan *castMessage

	// statusUpdates is a fan-out of unsolicited RECEIVER_STATUS messages.
	statusUpdates chan *castMessage

	closeOnce sync.Once
	closed    chan struct{}
}

func newConn(host string, port int, log *slog.Logger) *conn {
	if port == 0 {
		port = defaultCastPort
	}
	if log == nil {
		log = slog.Default()
	}
	return &conn{
		host:          host,
		port:          port,
		log:           log,
		openChannels:  make(map[string]bool),
		pending:       make(map[int]chan *castMessage),
		statusUpdates: make(chan *castMessage, connectBacklog),
		closed:        make(chan struct{}),
	}
}

// connect dials the device and starts the background read and heartbeat
// loops. It is safe to call connect again after close.
func (c *conn) connect() error {
	dialer := &net.Dialer{Timeout: dialTimeout}
	tlsConn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", c.host, c.port), &tls.Config{
		// Chromecast devices use self-signed certificates.
		InsecureSkipVerify: true, //nolint:gosec
	})
	if err != nil {
		return fmt.Errorf("dial %s:%d: %w", c.host, c.port, err)
	}

	c.mu.Lock()
	c.tlsConn = tlsConn
	c.connected = true
	c.closed = make(chan struct{})
	c.mu.Unlock()

	go c.readLoop()
	go c.heartbeatLoop()

	return nil
}

// close terminates the connection and background loops.
func (c *conn) close() error {
	c.mu.Lock()
	tlsConn := c.tlsConn
	c.connected = false
	c.openChannels = make(map[string]bool)
	c.mu.Unlock()

	c.closeOnce.Do(func() { close(c.closed) })

	if tlsConn != nil {
		return tlsConn.Close()
	}
	return nil
}

func (c *conn) isConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// ensureVirtualConnection opens a virtual connection to destinationID if one
// isn't already open. The Cast protocol multiplexes several logical
// connections (platform + one per running app) over a single TCP/TLS
// connection, and each must be explicitly "connected" before messages will
// be delivered.
func (c *conn) ensureVirtualConnection(destinationID string) error {
	c.mu.Lock()
	already := c.openChannels[destinationID]
	if !already {
		c.openChannels[destinationID] = true
	}
	c.mu.Unlock()

	if already {
		return nil
	}

	payload := map[string]any{
		"type":   "CONNECT",
		"origin": map[string]any{},
	}
	return c.sendRaw(destinationID, nsConnection, payload)
}

func (c *conn) closeVirtualConnection(destinationID string) {
	c.mu.Lock()
	open := c.openChannels[destinationID]
	delete(c.openChannels, destinationID)
	c.mu.Unlock()

	if !open {
		return
	}
	_ = c.sendRaw(destinationID, nsConnection, map[string]any{
		"type":   "CLOSE",
		"origin": map[string]any{},
	})
}

// nextRequestID returns a monotonically increasing request id, as required
// by the Cast protocol to correlate requests with responses.
func (c *conn) nextRequestID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestID += requestIDCounter
	return c.requestID
}

// send marshals payload to JSON, ensures the virtual connection to
// destinationID is open, and writes the framed CastMessage to the wire.
func (c *conn) send(destinationID, namespace string, payload any) error {
	if err := c.ensureVirtualConnection(destinationID); err != nil {
		return err
	}
	return c.sendRaw(destinationID, namespace, payload)
}

func (c *conn) sendRaw(destinationID, namespace string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	msg := &castMessage{
		ProtocolVersion: 0,
		SourceID:        senderID,
		DestinationID:   destinationID,
		Namespace:       namespace,
		PayloadUTF8:     string(data),
	}
	frame := msg.marshal()

	c.mu.Lock()
	tlsConn := c.tlsConn
	c.mu.Unlock()
	if tlsConn == nil {
		return errNotConnected
	}

	_ = tlsConn.SetWriteDeadline(time.Now().Add(writeTimeout))

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(frame)))
	if _, err := tlsConn.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if _, err := tlsConn.Write(frame); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}
	c.log.Debug("chromecast: sent message", "destination", destinationID, "namespace", namespace, "payload", string(data))
	return nil
}

// waitForRequest registers a channel that will receive the reply matching
// requestID, delivered by readLoop.
func (c *conn) waitForRequest(requestID int) chan *castMessage {
	ch := make(chan *castMessage, 1)
	c.mu.Lock()
	c.pending[requestID] = ch
	c.mu.Unlock()
	return ch
}

func (c *conn) forgetRequest(requestID int) {
	c.mu.Lock()
	delete(c.pending, requestID)
	c.mu.Unlock()
}

func (c *conn) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			if err := c.sendRaw(platformID, nsHeartbeat, map[string]any{"type": "PING"}); err != nil {
				c.log.Warn("chromecast: heartbeat failed", "error", err)
				return
			}
		}
	}
}

func (c *conn) readLoop() {
	for {
		c.mu.Lock()
		tlsConn := c.tlsConn
		c.mu.Unlock()
		if tlsConn == nil {
			return
		}

		_ = tlsConn.SetReadDeadline(time.Now().Add(readIdleTimeout))

		var lenBuf [4]byte
		if _, err := io.ReadFull(tlsConn, lenBuf[:]); err != nil {
			select {
			case <-c.closed:
			default:
				c.log.Debug("chromecast: read loop stopped", "error", err)
			}
			return
		}
		length := binary.BigEndian.Uint32(lenBuf[:])
		if length == 0 || length > 1<<20 {
			continue
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(tlsConn, payload); err != nil {
			c.log.Debug("chromecast: failed reading payload", "error", err)
			return
		}

		msg, err := unmarshalCastMessage(payload)
		if err != nil {
			c.log.Debug("chromecast: failed decoding CastMessage", "error", err)
			continue
		}
		c.handleMessage(msg)
	}
}

func (c *conn) handleMessage(msg *castMessage) {
	var env message
	if err := json.Unmarshal([]byte(msg.PayloadUTF8), &env); err != nil {
		c.log.Debug("chromecast: failed decoding payload envelope", "error", err)
		return
	}

	switch env.Type {
	case "PING":
		_ = c.sendRaw(msg.SourceID, msg.Namespace, map[string]any{"type": "PONG"})
		return
	case "PONG":
		return
	case "CLOSE":
		return
	}

	if env.RequestID != 0 {
		c.mu.Lock()
		ch, ok := c.pending[env.RequestID]
		delete(c.pending, env.RequestID)
		c.mu.Unlock()
		if ok {
			ch <- msg
			return
		}
	}

	if env.Type == "RECEIVER_STATUS" {
		select {
		case c.statusUpdates <- msg:
		default:
		}
	}
}
