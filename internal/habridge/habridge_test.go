package habridge

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stein155/retouch/internal/sim"
	"github.com/stein155/retouch/internal/speaker"
)

// fakeBroker is a throwaway MQTT broker for tests: it completes the handshake,
// records every retained PUBLISH by topic, and can push a command to the client. It
// re-implements just enough of the wire format (QoS 0) to avoid depending on the
// mqtt package internals.
type fakeBroker struct {
	ln net.Listener

	mu   sync.Mutex
	pub  map[string]string
	conn net.Conn
}

func newFakeBroker(t *testing.T) *fakeBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &fakeBroker{ln: ln, pub: map[string]string{}}
	go b.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return b
}

func (b *fakeBroker) addr() (host string, port int) {
	a := b.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (b *fakeBroker) accept() {
	conn, err := b.ln.Accept()
	if err != nil {
		return
	}
	br := bufio.NewReader(conn)
	// CONNECT
	if _, _, err := readFrame(br); err != nil {
		return
	}
	// CONNACK
	if _, err := conn.Write([]byte{2 << 4, 2, 0, 0}); err != nil {
		return
	}
	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()

	for {
		typ, body, err := readFrame(br)
		if err != nil {
			return
		}
		if typ == 3 { // PUBLISH (QoS 0: no packet id)
			topic, payload := parsePub(body)
			b.mu.Lock()
			b.pub[topic] = string(payload)
			b.mu.Unlock()
		}
	}
}

// published returns the last retained payload seen for topic.
func (b *fakeBroker) published(topic string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.pub[topic]
	return v, ok
}

// send pushes a PUBLISH (a command) to the connected client.
func (b *fakeBroker) send(t *testing.T, topic, payload string) {
	t.Helper()
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		t.Fatal("broker has no client connection yet")
	}
	var vh []byte
	vh = append(vh, byte(len(topic)>>8), byte(len(topic)))
	vh = append(vh, topic...)
	vh = append(vh, payload...)
	frame := append([]byte{3 << 4}, encodeRemaining(len(vh))...)
	frame = append(frame, vh...)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("broker send: %v", err)
	}
}

func readFrame(br *bufio.Reader) (typ byte, body []byte, err error) {
	first, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var n, mult int
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		n += int(b&0x7f) << mult
		if b&0x80 == 0 {
			break
		}
		mult += 7
	}
	body = make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return 0, nil, err
	}
	return first >> 4, body, nil
}

func parsePub(body []byte) (topic string, payload []byte) {
	if len(body) < 2 {
		return "", nil
	}
	n := int(body[0])<<8 | int(body[1])
	if len(body) < 2+n {
		return "", nil
	}
	return string(body[2 : 2+n]), body[2+n:]
}

func encodeRemaining(n int) []byte {
	var out []byte
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		out = append(out, d)
		if n == 0 {
			return out
		}
	}
}

// newSimSpeaker starts the SoundTouch simulator and returns a Client aimed at it.
func newSimSpeaker(t *testing.T) *speaker.Client {
	t.Helper()
	sp := sim.New()
	ts := httptest.NewServer(sp.Handler())
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return speaker.New(u.Host) // host:port -> Client uses that REST port
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBridgeDiscoveryAndCommands(t *testing.T) {
	broker := newFakeBroker(t)
	host, port := broker.addr()
	sp := newSimSpeaker(t)

	cfg := Config{
		Enabled:         true,
		Host:            host,
		Port:            port,
		BaseTopic:       "retouch/test",
		DiscoveryPrefix: "homeassistant",
	}
	b := New(sp, func() Config { return cfg }, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	// The simulator's device id (see internal/sim).
	const dev = "F4E11E3B013F"

	// Discovery configs are published for the entities.
	waitFor(t, "volume discovery", func() bool {
		_, ok := broker.published("homeassistant/number/" + dev + "/volume/config")
		return ok
	})
	for _, topic := range []string{
		"homeassistant/switch/" + dev + "/power/config",
		"homeassistant/select/" + dev + "/preset/config",
		"homeassistant/button/" + dev + "/play/config",
		"homeassistant/sensor/" + dev + "/station/config",
	} {
		if _, ok := broker.published(topic); !ok {
			t.Errorf("missing discovery config: %s", topic)
		}
	}

	// Availability comes up online.
	waitFor(t, "availability online", func() bool {
		v, ok := broker.published("retouch/test/availability")
		return ok && v == "online"
	})

	// Volume state is published.
	waitFor(t, "volume state", func() bool {
		_, ok := broker.published("retouch/test/volume/state")
		return ok
	})

	// A volume command routes through to the speaker.
	broker.send(t, "retouch/test/volume/set", "37")
	waitFor(t, "volume applied to speaker", func() bool {
		v, err := sp.Volume(context.Background())
		return err == nil && v == 37
	})

	// The update entity is absent when no updater is wired in.
	if _, ok := broker.published("homeassistant/update/" + dev + "/update/config"); ok {
		t.Error("update entity should not be published without an updater")
	}
}

// fakeUpdater implements habridge.Updater for tests.
type fakeUpdater struct {
	installed, latest string
	called            chan struct{}
}

func (f *fakeUpdater) UpdateInfo(context.Context) (string, string, string, bool, error) {
	return f.installed, f.latest, "https://example.test/tag/" + f.latest, true, nil
}

func (f *fakeUpdater) UpdateToLatest(context.Context) error {
	select {
	case f.called <- struct{}{}:
	default:
	}
	return nil
}

func TestBridgePublishesEnrichedTrack(t *testing.T) {
	broker := newFakeBroker(t)
	host, port := broker.addr()
	sp := newSimSpeaker(t)

	cfg := Config{Enabled: true, Host: host, Port: port, BaseTopic: "retouch/np", DiscoveryPrefix: "homeassistant"}
	b := New(sp, func() Config { return cfg }, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Enriched source: the live track the speaker's raw read never carries.
	b.SetNowPlaying(func(context.Context) (*speaker.NowPlaying, error) {
		return &speaker.NowPlaying{Source: "TUNEIN", Station: "Radio X", Track: "Song Y", Artist: "Artist Z", PlayStatus: "PLAY_STATE"}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	waitFor(t, "track state", func() bool {
		v, ok := broker.published("retouch/np/track")
		return ok && v == "Song Y"
	})
	if v, _ := broker.published("retouch/np/artist"); v != "Artist Z" {
		t.Errorf("artist = %q, want Artist Z", v)
	}
	if v, _ := broker.published("retouch/np/station"); v != "Radio X" {
		t.Errorf("station = %q, want Radio X", v)
	}
}

func TestBridgeUpdateEntity(t *testing.T) {
	broker := newFakeBroker(t)
	host, port := broker.addr()
	sp := newSimSpeaker(t)

	up := &fakeUpdater{installed: "v1.0.0", latest: "v1.1.0", called: make(chan struct{}, 1)}
	cfg := Config{Enabled: true, Host: host, Port: port, BaseTopic: "retouch/up", DiscoveryPrefix: "homeassistant"}
	b := New(sp, func() Config { return cfg }, up, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	const dev = "F4E11E3B013F"
	waitFor(t, "update discovery", func() bool {
		_, ok := broker.published("homeassistant/update/" + dev + "/update/config")
		return ok
	})

	// The state carries both versions so HA can show the update as available.
	waitFor(t, "update state", func() bool {
		v, ok := broker.published("retouch/up/update/state")
		return ok && strings.Contains(v, `"installed_version":"v1.0.0"`) && strings.Contains(v, `"latest_version":"v1.1.0"`)
	})

	// Installing from HA calls through to the updater.
	broker.send(t, "retouch/up/update/install", "install")
	select {
	case <-up.called:
	case <-time.After(3 * time.Second):
		t.Fatal("update install was not invoked")
	}
}
