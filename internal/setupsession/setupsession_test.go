package setupsession

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeSpeaker is a hand-rolled WebSocket server standing in for the firmware: it
// completes the RFC 6455 handshake, records every envelope it receives and acks each
// one the way the speaker does (echoing the requestID).
type fakeSpeaker struct {
	t           *testing.T
	ln          net.Listener
	got         chan string
	errorOn     string // route to answer with <error/> instead of an ack
	pingOnce    bool   // send a ping + a pushed notification before the first ack
	bogusAccept bool   // answer 101 with a non-RFC Sec-WebSocket-Accept, like the firmware
	subprotocol string // subprotocol to echo; empty means "gabbo"
}

func newFakeSpeaker(t *testing.T) *fakeSpeaker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeSpeaker{t: t, ln: ln, got: make(chan string, 32)}
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

// host returns the "127.0.0.1" the client should dial. The client always appends
// :8080, so the test rewires the port by dialling through a fixed local address.
func (f *fakeSpeaker) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSpeaker) serve() {
	go func() {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		br := bufio.NewReader(conn)

		req, err := http.ReadRequest(br)
		if err != nil {
			f.t.Errorf("read handshake: %v", err)
			return
		}
		sum := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + wsGUID)) //nolint:gosec // handshake constant
		accept := base64.StdEncoding.EncodeToString(sum[:])
		if f.bogusAccept {
			// What the real firmware does: 101 with an accept value that is not the
			// RFC 6455 hash of our key. The client must proceed anyway.
			accept = "ICX+Yqv66kxgM0FcWaLWlFLwTAI="
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Protocol: " + orGabbo(f.subprotocol) + "\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := io.WriteString(conn, resp); err != nil {
			return
		}

		first := true
		for {
			msg, opcode, err := readClientFrame(br)
			if err != nil {
				return
			}
			switch opcode {
			case 0x1: // text: an envelope to ack
			case 0x8: // close: client is done
				return
			default: // the pong answering our ping, etc.
				continue
			}
			f.got <- msg

			if first && f.pingOnce {
				first = false
				// A ping and a pushed notification must not be mistaken for the ack.
				_ = writeServerFrame(conn, 0x9, "")
				_ = writeServerFrame(conn, 0x1, `<updates deviceID="AABB"><nowPlayingUpdated/></updates>`)
			}
			id := between(msg, `requestID="`, `"`)
			route := between(msg, `url="`, `"`)
			if f.errorOn != "" && route == f.errorOn {
				_ = writeServerFrame(conn, 0x1, `<msg><header/><body><error value="7">setup refused</error></body></msg>`)
				continue
			}
			_ = writeServerFrame(conn, 0x1, `<msg><header><response requestID="`+id+`"/></header><body><status>/`+route+`</status></body></msg>`)
		}
	}()
}

func readClientFrame(br *bufio.Reader) (string, byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return "", 0, err
	}
	opcode := head[0] & 0x0F
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return "", 0, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return "", 0, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	if head[1]&0x80 == 0 {
		return "", 0, errUnmasked
	}
	if _, err := io.ReadFull(br, mask[:]); err != nil {
		return "", 0, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return "", 0, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return string(payload), opcode, nil
}

var errUnmasked = errUnmaskedType{}

type errUnmaskedType struct{}

func (errUnmaskedType) Error() string { return "client frame was not masked (RFC 6455 violation)" }

func writeServerFrame(w io.Writer, opcode byte, payload string) error {
	frame := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, byte(n))
	default:
		frame = append(frame, 126, byte(n>>8), byte(n))
	}
	_, err := w.Write(append(frame, payload...))
	return err
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// runAgainst starts the fake and points Run at it via Plan.Port.
func runAgainst(t *testing.T, f *fakeSpeaker, p Plan) error {
	t.Helper()
	f.serve()
	p.Port = f.port()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Run(ctx, p, nil)
}

func TestRunDrivesFullStateMachine(t *testing.T) {
	f := newFakeSpeaker(t)
	f.pingOnce = true

	err := runAgainst(t, f, Plan{
		Host:       "127.0.0.1",
		DeviceID:   "AABBCCDDEEFF",
		AccountID:  "1234567",
		AuthToken:  "retouch",
		Name:       "Keuken",
		BoseServer: "http://127.0.0.1:9080",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{
		`<setupState state="SETUP_START"/>`,
		`<setupState state="SETUP_IDENTIFY_DEVICE_ENTER" timeout="300000"/>`,
		`<sysLanguage>2</sysLanguage>`,
		`<setupState state="SETUP_ENTER"/>`,
		`<setupState state="SETUP_IDENTIFY_DEVICE_LEAVE"/>`,
		`<name>Keuken</name>`,
		`<accountId>1234567</accountId>`,
		`<setupState state="SETUP_LEAVE"/>`,
		`url="pushCustomerSupportInfoToMarge" method="GET"`,
	}
	for i, w := range want {
		select {
		case got := <-f.got:
			if !strings.Contains(got, w) {
				t.Fatalf("frame %d = %s, want it to contain %s", i, got, w)
			}
			if !strings.Contains(got, `deviceID="AABBCCDDEEFF"`) {
				t.Fatalf("frame %d missing deviceID: %s", i, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("frame %d (%s) never arrived", i, w)
		}
	}

	// SETUP_LEAVE is the step that actually ends setup mode; assert we did not stop early.
	select {
	case extra := <-f.got:
		t.Fatalf("unexpected extra frame: %s", extra)
	default:
	}
}

func TestRunReportsDeviceError(t *testing.T) {
	f := newFakeSpeaker(t)
	f.errorOn = "setMargeAccount"

	err := runAgainst(t, f, Plan{Host: "127.0.0.1", DeviceID: "AABB", AccountID: "1234567"})
	if err == nil {
		t.Fatal("Run should fail when the device rejects a step")
	}
	if !strings.Contains(err.Error(), "setMargeAccount") || !strings.Contains(err.Error(), "device rejected") {
		t.Fatalf("err = %v, want it to name the rejected step", err)
	}
}

func TestRunRequiresIdentity(t *testing.T) {
	if err := Run(context.Background(), Plan{Host: "127.0.0.1", AccountID: "1234567"}, nil); err == nil {
		t.Error("Run without deviceID should fail")
	}
	if err := Run(context.Background(), Plan{Host: "127.0.0.1", DeviceID: "AABB"}, nil); err == nil {
		t.Error("Run without accountID should fail")
	}
}

func TestPairXMLExtras(t *testing.T) {
	got := pairXML(Plan{AccountID: "1234567", AuthToken: "tok", BoseServer: "http://127.0.0.1:9080/"})
	for _, want := range []string{
		`<accountId>1234567</accountId>`,
		`<userAuthToken>tok</userAuthToken>`,
		`<boseServer>http://127.0.0.1:9080</boseServer>`,
		`<updateServer>http://127.0.0.1:9080/updates/soundtouch</updateServer>`,
		`<accountEmail>local@retouch.invalid</accountEmail>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pairXML missing %s: %s", want, got)
		}
	}
	if bare := pairXML(Plan{AccountID: "1234567"}); strings.Contains(bare, "boseServer") {
		t.Errorf("pairXML without BoseServer should stay minimal: %s", bare)
	}
}

// The speaker firmware answers the upgrade with 101 and the right subprotocol but an
// Sec-WebSocket-Accept that is not the RFC 6455 hash of our key. Verifying that value
// rejected the only server we care about, so the client must not verify it.
func TestRunAcceptsNonRFCHandshake(t *testing.T) {
	f := newFakeSpeaker(t)
	f.bogusAccept = true

	if err := runAgainst(t, f, Plan{Host: "127.0.0.1", DeviceID: "AABB", AccountID: "1234567"}); err != nil {
		t.Fatalf("Run should tolerate the firmware's non-RFC Sec-WebSocket-Accept, got: %v", err)
	}
}

// A peer that does not echo the "gabbo" subprotocol is not the firmware — most likely
// ReTouch's own web UI answering the redirected :8080 — and must be rejected.
func TestRunRejectsWrongSubprotocol(t *testing.T) {
	f := newFakeSpeaker(t)
	f.subprotocol = "http"

	err := runAgainst(t, f, Plan{Host: "127.0.0.1", DeviceID: "AABB", AccountID: "1234567"})
	if err == nil {
		t.Fatal("Run should reject a peer that negotiates a different subprotocol")
	}
	if !strings.Contains(err.Error(), "subprotocol") {
		t.Fatalf("err = %v, want it to mention the subprotocol", err)
	}
}

func orGabbo(s string) string {
	if s == "" {
		return "gabbo"
	}
	return s
}
