package display

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stein155/retouch/oled"
)

func newTestManager(t *testing.T, standby bool) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fb0")
	orig := bytes.Repeat([]byte{7}, oled.Width*oled.Height)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), path, "", func(context.Context) bool { return standby },
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return m, path
}

func TestUnavailableIsNoop(t *testing.T) {
	m := New(context.Background(), filepath.Join(t.TempDir(), "missing"), "", nil,
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if m.Available() {
		t.Fatal("missing fb reported available")
	}
	m.Notify(Content{Text: "x"}, time.Second) // must not panic
	var nilM *Manager
	if nilM.Available() {
		t.Fatal("nil manager available")
	}
	nilM.Notify(Content{}, 0)
	nilM.SetStandby("x", Content{})
	nilM.ClearStandby("x")
}

func TestStandbyShownAndRestored(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "morgen groen"})
	m.step(context.Background())
	after, _ := os.ReadFile(path)
	if bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("standby content not drawn")
	}
	m.ClearStandby("afvalwijzer")
	m.step(context.Background())
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("panel not restored after clear")
	}
}

func TestNotifyBeatsStandbyAndExpires(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "standby"})
	m.step(context.Background())
	standbyFrame, _ := os.ReadFile(path)

	m.Notify(Content{Icon: "bell", Text: "iemand aan de deur"}, time.Second)
	m.step(context.Background())
	notifyFrame, _ := os.ReadFile(path)
	if bytes.Equal(notifyFrame, standbyFrame) {
		t.Fatal("notification did not take over")
	}

	m.mu.Lock()
	m.notifyUntil = time.Now().Add(-time.Second) // fast-forward expiry
	m.mu.Unlock()
	m.step(context.Background())
	back, _ := os.ReadFile(path)
	if !bytes.Equal(back, standbyFrame) {
		t.Fatal("standby screen not back after notification expired")
	}
}

func TestNotPlayingHidesStandby(t *testing.T) {
	m, path := newTestManager(t, false) // speaker playing
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "standby"})
	m.step(context.Background())
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("standby content drawn while playing")
	}
}

func TestLastOwnerWins(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("a", Content{Icon: "groen", Text: "a"})
	m.SetStandby("b", Content{Icon: "bell", Text: "b"})
	m.step(context.Background())
	withB, _ := os.ReadFile(path)
	m.ClearStandby("b")
	m.step(context.Background())
	withA, _ := os.ReadFile(path)
	if bytes.Equal(withA, withB) {
		t.Fatal("clearing the winning owner did not fall back to the other")
	}
	if !bytes.Equal(withB, render(Content{Icon: "bell", Text: "b"})) {
		t.Fatal("most recently set owner was not the one shown")
	}
}

func TestRenderUnknownIconFallsBack(t *testing.T) {
	if len(render(Content{Icon: "nope", Text: "x"})) != oled.Width*oled.Height {
		t.Fatal("bad frame size")
	}
}

// TestDumpScreens writes preview PNGs of every built-in icon to the ICONDUMP
// dir for visual checks.
func TestDumpScreens(t *testing.T) {
	dir := os.Getenv("ICONDUMP")
	if dir == "" {
		t.Skip("set ICONDUMP to dump preview PNGs")
	}
	for name := range icons {
		frame := render(Content{Icon: name, Text: "voorbeeld " + name})
		img := &image.Gray{Pix: frame, Stride: oled.Width, Rect: image.Rect(0, 0, oled.Width, oled.Height)}
		f, err := os.Create(filepath.Join(dir, "screen-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func TestClockSuppressedWhileShownAndRestored(t *testing.T) {
	var mu sync.Mutex
	current := `<clockDisplay><clockConfig timezoneInfo="Europe/Amsterdam" userEnable="true" timeFormat="TIME_FORMAT_24HOUR_ID" userOffsetMinute="0" brightnessLevel="70" userUtcTime="0" /></clockDisplay>`
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clockDisplay" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			posts = append(posts, string(b))
			current = string(b)
			return
		}
		_, _ = w.Write([]byte(current))
	}))
	defer srv.Close()

	m, _ := newTestManager(t, true)
	m.speaker = strings.TrimPrefix(srv.URL, "http://")

	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "x"})
	m.step(context.Background())
	mu.Lock()
	if len(posts) != 1 || !strings.Contains(posts[0], `userEnable="false"`) {
		t.Fatalf("clock not suppressed: %q", posts)
	}
	mu.Unlock()

	m.step(context.Background()) // no re-suppress while already saved
	mu.Lock()
	if len(posts) != 1 {
		t.Fatalf("re-suppressed: %d posts", len(posts))
	}
	mu.Unlock()

	m.ClearStandby("afvalwijzer")
	m.step(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 || !strings.Contains(posts[1], `userEnable="true"`) {
		t.Fatalf("clock not restored: %q", posts)
	}
}

func TestClockUntouchedWhenUserDisabled(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			return
		}
		_, _ = w.Write([]byte(`<clockDisplay><clockConfig userEnable="false" /></clockDisplay>`))
	}))
	defer srv.Close()
	m, _ := newTestManager(t, true)
	m.speaker = strings.TrimPrefix(srv.URL, "http://")
	m.SetStandby("a", Content{Icon: "groen", Text: "x"})
	m.step(context.Background())
	if posts != 0 {
		t.Fatal("posted clock config although the user had the clock off")
	}
}

func TestClockRestoredEvenWithoutShownFrame(t *testing.T) {
	current := `<clockDisplay><clockConfig userEnable="true" /></clockDisplay>`
	var posts []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			posts = append(posts, string(b))
			current = string(b)
			return
		}
		_, _ = w.Write([]byte(current))
	}))
	defer srv.Close()

	// A manager whose framebuffer path is unwritable: suppress succeeds but
	// Draw never lands, so shown stays nil.
	m, _ := newTestManager(t, true)
	m.speaker = strings.TrimPrefix(srv.URL, "http://")
	m.fb = oled.NewFramebuffer(filepath.Join(t.TempDir(), "missing", "fb0"))

	m.SetStandby("a", Content{Icon: "groen", Text: "x"})
	m.step(context.Background()) // suppresses, draw fails
	m.ClearStandby("a")
	m.step(context.Background()) // must restore the clock anyway
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 || !strings.Contains(posts[1], `userEnable="true"`) {
		t.Fatalf("clock left suppressed: %q", posts)
	}
}
