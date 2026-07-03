package mqtt

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestRemainingLengthRoundTrip(t *testing.T) {
	// Boundaries of each variable-length-integer size class (MQTT §2.2.3).
	for _, n := range []int{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455} {
		encoded := appendRemainingLength(nil, n)
		got, err := readRemainingLength(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("n=%d: decode error: %v", n, err)
		}
		if got != n {
			t.Fatalf("n=%d: round-trip got %d", n, got)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	for _, s := range []string{"", "MQTT", "retouch/F4E11E3B013F/volume/set", "héllo ✓"} {
		encoded := appendString(nil, s)
		got, rest, ok := takeString(encoded)
		if !ok || got != s || len(rest) != 0 {
			t.Fatalf("%q: got %q ok=%v rest=%d", s, got, ok, len(rest))
		}
	}
}

func TestTakeStringShort(t *testing.T) {
	if _, _, ok := takeString([]byte{0x00}); ok {
		t.Fatal("takeString accepted a truncated length prefix")
	}
	if _, _, ok := takeString([]byte{0x00, 0x05, 'a'}); ok {
		t.Fatal("takeString accepted a body shorter than its length prefix")
	}
}

// parseConnect pulls the fields the test cares about out of a CONNECT body.
func parseConnect(t *testing.T, body []byte) (clientID, willTopic, username, password string, willRetain bool) {
	t.Helper()
	proto, rest, ok := takeString(body)
	if !ok || proto != "MQTT" {
		t.Fatalf("CONNECT protocol name = %q ok=%v", proto, ok)
	}
	if len(rest) < 4 {
		t.Fatal("CONNECT variable header too short")
	}
	flags := rest[1]
	rest = rest[4:] // skip level + flags + 2-byte keepalive
	clientID, rest, _ = takeString(rest)
	if flags&0x04 != 0 { // will flag
		willTopic, rest, _ = takeString(rest)
		_, rest, _ = takeString(rest) // will payload
		willRetain = flags&0x20 != 0
	}
	if flags&0x80 != 0 { // username
		username, rest, _ = takeString(rest)
	}
	if flags&0x40 != 0 { // password
		password, _, _ = takeString(rest)
	}
	return
}

// TestConnectPublishSubscribe drives the client against a fake broker built on the
// same codec: it verifies the CONNECT payload, that Subscribe/Publish are framed as
// the right packet types, and that an inbound PUBLISH reaches the handler.
func TestConnectPublishSubscribe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type pkt struct {
		typ, flags byte
		body       []byte
	}
	connectCh := make(chan pkt, 1)
	clientPkts := make(chan pkt, 4)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)

		typ, flags, body, err := readPacket(br)
		if err != nil {
			return
		}
		connectCh <- pkt{typ, flags, body}

		// CONNACK: fixed header + [session present, return code].
		if _, err := conn.Write([]byte{pktConnack << 4, 2, 0, 0}); err != nil {
			return
		}

		// Push a PUBLISH down to the client.
		var vh []byte
		vh = appendString(vh, "retouch/x/cmd")
		vh = append(vh, []byte("hello")...)
		frame := append([]byte{pktPublish << 4}, appendRemainingLength(nil, len(vh))...)
		frame = append(frame, vh...)
		if _, err := conn.Write(frame); err != nil {
			return
		}

		// Read the SUBSCRIBE and PUBLISH the client sends back.
		for i := 0; i < 2; i++ {
			typ, flags, body, err := readPacket(br)
			if err != nil {
				return
			}
			clientPkts <- pkt{typ, flags, body}
		}
	}()

	msgs := make(chan [2]string, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Connect(ctx, Options{
		Addr:     ln.Addr().String(),
		ClientID: "cid",
		Username: "user",
		Password: "pass",
		Will:     &Will{Topic: "retouch/x/availability", Payload: []byte("offline"), Retain: true},
		Handler:  func(topic string, payload []byte) { msgs <- [2]string{topic, string(payload)} },
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Disconnect()

	select {
	case cp := <-connectCh:
		if cp.typ != pktConnect {
			t.Fatalf("first packet type = %d, want CONNECT", cp.typ)
		}
		id, willTopic, user, pass, retain := parseConnect(t, cp.body)
		if id != "cid" || user != "user" || pass != "pass" {
			t.Fatalf("CONNECT creds: id=%q user=%q pass=%q", id, user, pass)
		}
		if willTopic != "retouch/x/availability" || !retain {
			t.Fatalf("CONNECT will: topic=%q retain=%v", willTopic, retain)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for CONNECT")
	}

	// Inbound PUBLISH must reach the handler.
	select {
	case m := <-msgs:
		if m[0] != "retouch/x/cmd" || m[1] != "hello" {
			t.Fatalf("handler got %v", m)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for inbound publish")
	}

	if err := c.Subscribe("retouch/x/#"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := c.Publish("retouch/x/state", []byte("v"), true); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := map[byte]pkt{}
	for i := 0; i < 2; i++ {
		select {
		case p := <-clientPkts:
			got[p.typ] = p
		case <-ctx.Done():
			t.Fatal("timed out waiting for client packets")
		}
	}
	if _, ok := got[pktSubscribe]; !ok {
		t.Fatal("broker never received a SUBSCRIBE")
	}
	pub, ok := got[pktPublish]
	if !ok {
		t.Fatal("broker never received a PUBLISH")
	}
	if pub.flags&0x01 == 0 {
		t.Fatal("PUBLISH retain flag not set")
	}
	topic, payload, ok := parsePublish(pub.flags, pub.body)
	if !ok || topic != "retouch/x/state" || string(payload) != "v" {
		t.Fatalf("PUBLISH parsed as topic=%q payload=%q ok=%v", topic, payload, ok)
	}
}
