// Package mqtt is a tiny, dependency-free MQTT 3.1.1 client — just enough for a
// memory-constrained device to publish state and subscribe to commands. It speaks
// only QoS 0 (the level Home Assistant discovery uses), over plain TCP or TLS.
//
// It is deliberately small: no persistent session, no QoS 1/2 acknowledgement
// tracking, no automatic reconnect. Reconnection is the caller's job (see
// internal/habridge), which lets the supervisor re-read config on every attempt.
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Packet type codes (MQTT 3.1.1 §2.2.1), in the high nibble of the fixed header.
const (
	pktConnect     = 1
	pktConnack     = 2
	pktPublish     = 3
	pktSuback      = 9
	pktSubscribe   = 8
	pktPingreq     = 12
	pktPingresp    = 13
	pktDisconnect  = 14
	protocolLevel4 = 0x04 // MQTT 3.1.1
)

// Will is the broker's Last Will and Testament: the message the broker publishes
// on the client's behalf if the connection drops without a clean DISCONNECT. The
// bridge uses it so Home Assistant sees the speaker go offline when ReTouch dies.
type Will struct {
	Topic   string
	Payload []byte
	Retain  bool
}

// Options configures a Connect.
type Options struct {
	Addr        string        // broker "host:port"
	ClientID    string        // client identifier (unique per broker)
	Username    string        // optional
	Password    string        // optional (only sent when Username is set)
	KeepAlive   time.Duration // 0 -> 60s; the broker disconnects after 1.5× this idle
	TLS         bool          // dial over TLS
	TLSConfig   *tls.Config   // optional; nil uses defaults for the host
	DialTimeout time.Duration // 0 -> 10s
	Will        *Will         // optional LWT
	// Handler receives every inbound PUBLISH. It runs on the read goroutine, so it
	// must not block; hand work off to another goroutine if it might.
	Handler func(topic string, payload []byte)
}

// Client is a live MQTT connection. It is safe for concurrent Publish/Subscribe.
type Client struct {
	conn      net.Conn
	reader    *bufio.Reader // persistent so buffered bytes survive between packets
	writeMu   sync.Mutex    // serialises writes to conn
	keepAlive time.Duration
	handler   func(topic string, payload []byte)

	nextID uint16 // packet ids for SUBSCRIBE (writeMu guards it)

	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// Connect dials the broker, performs the MQTT handshake, and starts the read and
// keepalive loops. The returned Client is ready for Publish/Subscribe. ctx bounds
// only the dial + handshake, not the connection lifetime.
func Connect(ctx context.Context, opts Options) (*Client, error) {
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = 60 * time.Second
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 10 * time.Second
	}

	dialer := net.Dialer{Timeout: opts.DialTimeout}
	var (
		conn net.Conn
		err  error
	)
	if opts.TLS {
		conn, err = tls.DialWithDialer(&dialer, "tcp", opts.Addr, opts.TLSConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", opts.Addr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial broker: %w", err)
	}

	c := &Client{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		keepAlive: opts.KeepAlive,
		handler:   opts.Handler,
		nextID:    1,
		done:      make(chan struct{}),
	}

	// Bound the handshake by the caller's context.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if err := c.writeConnect(opts); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.readConnack(); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{}) // clear; the read loop runs unbounded

	go c.readLoop()
	go c.keepAliveLoop()
	return c, nil
}

// Done is closed when the connection is lost or Disconnect is called. Err reports
// the cause (nil for a clean local Disconnect).
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns the error that closed the connection, or nil if it is still up or was
// closed cleanly.
func (c *Client) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

// Publish sends an application message at QoS 0.
func (c *Client) Publish(topic string, payload []byte, retain bool) error {
	var vh []byte
	vh = appendString(vh, topic) // no packet id at QoS 0
	flags := byte(0)
	if retain {
		flags |= 0x01
	}
	return c.writePacket(pktPublish, flags, append(vh, payload...))
}

// Subscribe registers interest in one or more topic filters at QoS 0.
func (c *Client) Subscribe(topics ...string) error {
	if len(topics) == 0 {
		return nil
	}
	c.writeMu.Lock()
	id := c.nextID
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1 // packet id 0 is illegal
	}
	c.writeMu.Unlock()

	payload := []byte{byte(id >> 8), byte(id)}
	for _, t := range topics {
		payload = appendString(payload, t)
		payload = append(payload, 0) // requested QoS 0
	}
	// SUBSCRIBE fixed-header flags are reserved and MUST be 0b0010.
	return c.writePacket(pktSubscribe, 0x02, payload)
}

// Disconnect sends DISCONNECT and closes the connection. Safe to call repeatedly.
func (c *Client) Disconnect() {
	c.writeMu.Lock()
	_ = c.writeFrameLocked(pktDisconnect, 0, nil)
	c.writeMu.Unlock()
	c.close(nil)
}

// --- handshake ---

func (c *Client) writeConnect(opts Options) error {
	var vh []byte
	vh = appendString(vh, "MQTT")
	vh = append(vh, protocolLevel4)

	var flags byte = 0x02 // clean session
	if opts.Will != nil {
		flags |= 0x04 // will flag
		if opts.Will.Retain {
			flags |= 0x20
		}
	}
	if opts.Username != "" {
		flags |= 0x80
		if opts.Password != "" {
			flags |= 0x40
		}
	}
	vh = append(vh, flags)
	ka := uint16(opts.KeepAlive / time.Second)
	vh = append(vh, byte(ka>>8), byte(ka))

	var payload []byte
	payload = appendString(payload, opts.ClientID)
	if opts.Will != nil {
		payload = appendString(payload, opts.Will.Topic)
		payload = appendBytes(payload, opts.Will.Payload)
	}
	if opts.Username != "" {
		payload = appendString(payload, opts.Username)
		if opts.Password != "" {
			payload = appendString(payload, opts.Password)
		}
	}
	return c.writePacket(pktConnect, 0, append(vh, payload...))
}

func (c *Client) readConnack() error {
	typ, _, body, err := readPacket(c.reader)
	if err != nil {
		return fmt.Errorf("read CONNACK: %w", err)
	}
	if typ != pktConnack {
		return fmt.Errorf("expected CONNACK, got packet type %d", typ)
	}
	if len(body) < 2 {
		return errors.New("short CONNACK")
	}
	if code := body[1]; code != 0 {
		return fmt.Errorf("connection refused: return code %d", code)
	}
	return nil
}

// --- loops ---

func (c *Client) readLoop() {
	for {
		typ, flags, body, err := readPacket(c.reader)
		if err != nil {
			c.close(err)
			return
		}
		switch typ {
		case pktPublish:
			topic, payload, ok := parsePublish(flags, body)
			if ok && c.handler != nil {
				c.handler(topic, payload)
			}
		case pktPingresp, pktSuback:
			// nothing to do (QoS 0)
		}
	}
}

func (c *Client) keepAliveLoop() {
	// Ping well within the keepalive window so the broker never times us out.
	interval := c.keepAlive * 3 / 4
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if err := c.writePacket(pktPingreq, 0, nil); err != nil {
				return // read loop will report the failure and close
			}
		}
	}
}

// --- framing ---

// writePacket serialises a full packet (fixed header + rest) under the write lock.
func (c *Client) writePacket(typ, flags byte, rest []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrameLocked(typ, flags, rest)
}

func (c *Client) writeFrameLocked(typ, flags byte, rest []byte) error {
	var frame []byte
	frame = append(frame, typ<<4|flags)
	frame = appendRemainingLength(frame, len(rest))
	frame = append(frame, rest...)
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.conn.Write(frame)
	if err != nil {
		c.close(err)
	}
	return err
}

func (c *Client) close(err error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		_ = c.conn.Close()
		close(c.done)
	})
}

// readPacket reads one whole MQTT packet: returns its type, header flags (low
// nibble) and the remaining bytes (variable header + payload). The reader must be
// the connection's single persistent bufio.Reader, so bytes buffered past one
// packet aren't lost before the next read.
func readPacket(br *bufio.Reader) (typ, flags byte, body []byte, err error) {
	first, err := br.ReadByte()
	if err != nil {
		return 0, 0, nil, err
	}
	n, err := readRemainingLength(br)
	if err != nil {
		return 0, 0, nil, err
	}
	body = make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return 0, 0, nil, err
	}
	return first >> 4, first & 0x0f, body, nil
}

// parsePublish extracts the topic and payload from a PUBLISH body. Only QoS 0 is
// supported, so there is no packet identifier between the topic and the payload.
func parsePublish(flags byte, body []byte) (topic string, payload []byte, ok bool) {
	if qos := (flags >> 1) & 0x03; qos != 0 {
		return "", nil, false // we never subscribe above QoS 0
	}
	topic, rest, ok := takeString(body)
	if !ok {
		return "", nil, false
	}
	return topic, rest, true
}

// appendRemainingLength encodes n as an MQTT variable-length integer (1–4 bytes).
func appendRemainingLength(b []byte, n int) []byte {
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 0x80
		}
		b = append(b, digit)
		if n == 0 {
			return b
		}
	}
}

// readRemainingLength decodes an MQTT variable-length integer.
func readRemainingLength(r io.ByteReader) (int, error) {
	var value, mult int
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&0x7f) * (1 << mult)
		if b&0x80 == 0 {
			return value, nil
		}
		mult += 7
	}
	return 0, errors.New("malformed remaining length")
}

// appendString appends a length-prefixed UTF-8 string (MQTT §1.5.3).
func appendString(b []byte, s string) []byte {
	b = append(b, byte(len(s)>>8), byte(len(s)))
	return append(b, s...)
}

// appendBytes appends a length-prefixed binary blob (same framing as a string).
func appendBytes(b []byte, p []byte) []byte {
	b = append(b, byte(len(p)>>8), byte(len(p)))
	return append(b, p...)
}

// takeString reads a length-prefixed string off the front of b, returning it and
// the remainder.
func takeString(b []byte) (s string, rest []byte, ok bool) {
	if len(b) < 2 {
		return "", nil, false
	}
	n := int(b[0])<<8 | int(b[1])
	if len(b) < 2+n {
		return "", nil, false
	}
	return string(b[2 : 2+n]), b[2+n:], true
}
