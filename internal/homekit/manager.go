package homekit

import (
	"context"
	"os"
	"sync"

	"log/slog"

	"github.com/stein155/retouch/internal/speaker"
)

// Manager runs the HomeKit bridge on demand so it can be toggled at runtime from
// the settings page — no relaunch (and so no rewritten autostart command) needed.
// Run blocks until its context is cancelled, so Start gives it a child context it
// can cancel from Stop.
type Manager struct {
	parent context.Context
	bc     *speaker.Client
	info   *speaker.Info
	cfg    Config
	log    *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc // non-nil while the bridge is running
	done    chan struct{}      // closed when the running bridge's goroutine exits
	running bool
}

// NewManager prepares a HomeKit manager bound to parent's lifetime. It does not
// start the bridge; call Start (e.g. when the persisted setting is on, or when the
// user enables it).
func NewManager(parent context.Context, bc *speaker.Client, info *speaker.Info, cfg Config, log *slog.Logger) *Manager {
	return &Manager{parent: parent, bc: bc, info: info, cfg: cfg, log: log}
}

// Start brings the bridge up if it isn't already. It returns immediately; the HAP
// server runs in its own goroutine until Stop (or the parent context) cancels it.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	ctx, cancel := context.WithCancel(m.parent)
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	m.running = true
	go func() {
		defer close(done)
		if err := Run(ctx, m.bc, m.info, m.cfg, m.log); err != nil && ctx.Err() == nil {
			m.log.Error("homekit bridge stopped", "err", err)
		}
	}()
}

// Stop tears the bridge down if it's running and waits for the HAP server to
// release its port. Pairing state persists on disk, so a later Start re-publishes
// the same accessory without re-pairing.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.running = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done // wait for ListenAndServe to return so the port is free
	}
}

// Reset clears the HomeKit pairing state and re-publishes the accessory as
// unpaired, so it becomes discoverable in the Home app again. Use it when a pairing
// was removed on the controller side (or never completed) and the accessory is left
// advertising as already-paired, which hides it from "Add Accessory".
func (m *Manager) Reset() error {
	m.mu.Lock()
	wasRunning := m.running
	m.mu.Unlock()

	m.Stop() // waits for shutdown so the store files aren't in use
	if m.cfg.StorageDir != "" {
		if err := os.RemoveAll(m.cfg.StorageDir); err != nil {
			return err
		}
	}
	if wasRunning {
		m.Start()
	}
	return nil
}

// Enabled reports whether the bridge is currently running.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Name is the accessory name shown in the Home app.
func (m *Manager) Name() string {
	name := m.cfg.Name
	if name == "" {
		name = m.info.Name
	}
	if name == "" {
		name = "ReTouch"
	}
	return name
}

// Code is the formatted HomeKit setup code (XXX-XX-XXX) to type into the Home app.
func (m *Manager) Code() string {
	return FmtPin(PinFor(m.cfg.Pin, m.info.DeviceID))
}

// SetupURI is the HomeKit QR-code payload ("X-HM://…"): scanning it in the Home
// app pairs the accessory using the same setup code Code renders.
func (m *Manager) SetupURI() string {
	return SetupURI(PinFor(m.cfg.Pin, m.info.DeviceID), SetupIDFor(m.info.DeviceID))
}
