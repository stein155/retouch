package setupsession

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// wsGUID is the RFC 6455 handshake constant a compliant server derives
// Sec-WebSocket-Accept from. Kept for the test's server side; see wsDial for why the
// client does not verify the value the SoundTouch firmware returns.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B10"

// wsConn is a minimal RFC 6455 client for text frames.
//
// ponytail: the firmware's setup protocol needs exactly one thing from WebSocket —
// send a text frame, read text frames until the ack. That is ~100 lines of stdlib,
// so ReTouch keeps its zero-dependency build instead of pulling in gorilla/websocket.
// Not implemented (and not needed here): outbound fragmentation, binary frames,
// permessage-deflate, concurrent writers.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// wsDial opens a WebSocket to host (host:port) at path "/" and negotiates subprotocol.
func wsDial(host, subprotocol string, timeout time.Duration) (*wsConn, error) {
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("handshake key: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(key)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	req := "GET / HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + nonce + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	if subprotocol != "" {
		req += "Sec-WebSocket-Protocol: " + subprotocol + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("handshake: got HTTP %d, want 101 (is another server on %s?)", resp.StatusCode, host)
	}
	// ponytail: Sec-WebSocket-Accept is deliberately NOT verified. The SoundTouch
	// firmware (WebServer, serverVersion=4) answers 101 with an accept value that is not
	// the RFC 6455 SHA-1 of our key — for key "AAAAAAAAAAAAAAAAAAAAAA==" it returns
	// "ICX+Yqv66kxgM0FcWaLWlFLwTAI=" where the RFC requires
	// "+JMwNIYfSoL3ldFfkHBl+6pC0RQ=" — so enforcing it rejects the very server we want.
	// The check exists to catch a confused proxy replaying a handshake; on a direct
	// socket to a known device on the LAN it buys nothing. The subprotocol echo below is
	// the useful signal instead: it distinguishes the firmware from anything else that
	// might answer this port (notably ReTouch's own web UI behind the :8080 redirect).
	if subprotocol != "" {
		if got := resp.Header.Get("Sec-WebSocket-Protocol"); !strings.EqualFold(got, subprotocol) {
			return nil, fmt.Errorf("handshake: peer negotiated subprotocol %q, want %q (not the speaker firmware?)", got, subprotocol)
		}
	}

	ok = true
	return &wsConn{conn: conn, br: br}, nil
}

// writeText sends s as a single masked text frame. Client frames must be masked.
func (w *wsConn) writeText(s string, deadline time.Time) error {
	if err := w.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	payload := []byte(s)
	head := []byte{0x81} // FIN + opcode 0x1 (text)
	switch n := len(payload); {
	case n < 126:
		head = append(head, byte(0x80|n))
	case n <= 0xFFFF:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		head = binary.BigEndian.AppendUint64(head, uint64(n))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("mask key: %w", err)
	}
	head = append(head, mask[:]...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := w.conn.Write(append(head, masked...)); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// readText returns the next complete text message, reassembling continuation frames.
// Ping frames are answered with a pong; pongs are skipped; a close frame ends the read.
func (w *wsConn) readText(deadline time.Time) (string, error) {
	var msg strings.Builder
	for {
		if err := w.conn.SetReadDeadline(deadline); err != nil {
			return "", err
		}
		var head [2]byte
		if _, err := io.ReadFull(w.br, head[:]); err != nil {
			return "", fmt.Errorf("read frame header: %w", err)
		}
		fin := head[0]&0x80 != 0
		opcode := head[0] & 0x0F
		masked := head[1]&0x80 != 0

		length := uint64(head[1] & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return "", fmt.Errorf("read frame length: %w", err)
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return "", fmt.Errorf("read frame length: %w", err)
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		if length > maxFrameBytes {
			return "", fmt.Errorf("frame too large: %d bytes", length)
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.br, mask[:]); err != nil {
				return "", fmt.Errorf("read frame mask: %w", err)
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return "", fmt.Errorf("read frame payload: %w", err)
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case 0x0, 0x1: // continuation, text
			msg.Write(payload)
			if fin {
				return msg.String(), nil
			}
		case 0x8: // close
			return "", io.EOF
		case 0x9: // ping — reply so the firmware keeps the session open
			if err := w.writeControl(0xA, payload, deadline); err != nil {
				return "", err
			}
		case 0xA: // pong
		default: // binary or reserved: not part of this protocol
			return "", fmt.Errorf("unexpected websocket opcode 0x%X", opcode)
		}
	}
}

// maxFrameBytes caps an inbound frame. Setup acks are a few hundred bytes; this only
// exists so a corrupt length header cannot make us allocate wildly.
const maxFrameBytes = 1 << 20

func (w *wsConn) writeControl(opcode byte, payload []byte, deadline time.Time) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	if err := w.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("mask key: %w", err)
	}
	frame := []byte{0x80 | opcode, byte(0x80 | len(payload))}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.conn.Write(frame)
	return err
}

// close sends a normal-closure frame (best effort) and closes the socket.
func (w *wsConn) close() error {
	_ = w.writeControl(0x8, []byte{0x03, 0xE8}, time.Now().Add(time.Second)) // 1000 = normal
	return w.conn.Close()
}
