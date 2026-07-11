// Package display owns the SoundTouch 20 front-panel OLED. It is the single
// writer to /dev/fb0: plugins and internal features hand it content (a
// built-in icon or custom sprite plus a line of text) and the manager decides
// what is actually on the panel — a short-lived notification wins over a
// standby screen, standby screens only show while the speaker is in standby,
// and the firmware's own frame is snapshotted and restored when nothing of
// ours should be visible. On models without the ST20 framebuffer everything
// here is a no-op (Available reports false).
package display

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/oled"
)

// A Content is one screen: an icon (built-in name or custom sprite) above a
// localized line of text.
type Content struct {
	Icon   string   // built-in icon name (see icons.go); ignored when Sprite is set
	Sprite []string // custom ASCII-art sprite: '#' white, '+' gray, ' ' transparent
	Text   string   // sentence drawn (wrapped, centered) under the icon
}

// Manager arbitrates the panel. Create with New; nil-safe: a nil *Manager
// ignores all calls and reports unavailable, so callers never need a guard.
type Manager struct {
	fb      *oled.Framebuffer
	avail   bool
	standby func(context.Context) bool // live speaker-in-standby check
	log     *slog.Logger

	mu          sync.Mutex
	notifyFrame []byte
	notifyUntil time.Time
	slots       map[string]Content // standby screens by owner
	slotOrder   []string           // owners, oldest first; last one shown
	shown       []byte             // frame currently on the panel (nil = firmware's)
}

// New builds the manager for fbPath (normally /dev/fb0) and starts its loop.
// standby reports whether the speaker is in standby; it is only called while
// standby content is registered.
func New(ctx context.Context, fbPath string, standby func(context.Context) bool, log *slog.Logger) *Manager {
	m := &Manager{
		fb:      oled.NewFramebuffer(fbPath),
		avail:   oled.Available(fbPath),
		standby: standby,
		log:     log,
		slots:   map[string]Content{},
	}
	if !m.avail {
		log.Info("no ST20 OLED framebuffer; display disabled", "path", fbPath)
		return m
	}
	go m.loop(ctx)
	return m
}

// Available reports whether this speaker has the ST20 panel.
func (m *Manager) Available() bool { return m != nil && m.avail }

// Notify shows content immediately — also over playing music — and restores
// whatever should be visible after d (clamped to 1..60s).
func (m *Manager) Notify(c Content, d time.Duration) {
	if !m.Available() {
		return
	}
	if d < time.Second {
		d = 8 * time.Second
	}
	if d > time.Minute {
		d = time.Minute
	}
	frame := render(c)
	m.mu.Lock()
	m.notifyFrame = frame
	m.notifyUntil = time.Now().Add(d)
	m.mu.Unlock()
}

// SetStandby registers owner's standby screen: it is shown while the speaker
// is in standby (unless a notification is up). With multiple owners the most
// recently set one wins.
func (m *Manager) SetStandby(owner string, c Content) {
	if !m.Available() || owner == "" {
		return
	}
	m.mu.Lock()
	if _, ok := m.slots[owner]; ok {
		for i, o := range m.slotOrder {
			if o == owner {
				m.slotOrder = append(m.slotOrder[:i], m.slotOrder[i+1:]...)
				break
			}
		}
	}
	m.slots[owner] = c
	m.slotOrder = append(m.slotOrder, owner)
	m.mu.Unlock()
}

// ClearStandby removes owner's standby screen.
func (m *Manager) ClearStandby(owner string) {
	if !m.Available() {
		return
	}
	m.mu.Lock()
	delete(m.slots, owner)
	for i, o := range m.slotOrder {
		if o == owner {
			m.slotOrder = append(m.slotOrder[:i], m.slotOrder[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

// loop drives the panel once a second: notification > standby screen >
// firmware's own frame.
func (m *Manager) loop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = m.fb.Restore()
			return
		case <-tick.C:
			m.step(ctx)
		}
	}
}

func (m *Manager) step(ctx context.Context) {
	m.mu.Lock()
	var want []byte
	if m.notifyFrame != nil && time.Now().Before(m.notifyUntil) {
		want = m.notifyFrame
	} else {
		m.notifyFrame = nil
		if len(m.slotOrder) > 0 {
			c := m.slots[m.slotOrder[len(m.slotOrder)-1]]
			m.mu.Unlock()
			if m.standby(ctx) {
				want = render(c)
			}
			m.mu.Lock()
		}
	}
	shown := m.shown
	m.mu.Unlock()

	if want == nil {
		if shown != nil {
			if err := m.fb.Restore(); err == nil {
				m.setShown(nil)
			}
		}
		return
	}
	if bytesEqual(shown, want) {
		return
	}
	if err := m.fb.Draw(want); err != nil {
		m.log.Warn("display draw failed", "err", err)
		return
	}
	m.setShown(want)
}

func (m *Manager) setShown(b []byte) {
	m.mu.Lock()
	m.shown = b
	m.mu.Unlock()
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// render draws content in the house layout: icon centered, text wrapped and
// centered near the bottom, thin border.
func render(c Content) []byte {
	art := c.Sprite
	if len(art) == 0 {
		art = icons[c.Icon]
	}
	if len(art) == 0 {
		art = icons["info"]
	}
	cv := oled.NewCanvas()
	lines := oled.Wrap(strings.ToUpper(c.Text), 20, 3)
	textTop := 90 - (len(lines)-1)*oled.TextHeight
	w, h := oled.SpriteSize(art)
	iconTop := textTop - h - 8
	if iconTop < 2 {
		iconTop = 2
	}
	cv.Sprite((oled.Width-w)/2, iconTop, art, 255, 150)
	for i, line := range lines {
		cv.TextCentered(textTop+i*oled.TextHeight, line, 255)
	}
	cv.Rect(0, 0, oled.Width-1, oled.Height-1, 70)
	return cv.Pix()
}
