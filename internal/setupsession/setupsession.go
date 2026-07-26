// Package setupsession drives the firmware's own SETUP state machine so the speaker
// leaves setup mode ("connect with the SoundTouch app") without the Bose app or cloud.
//
// A single POST to :8090/setMargeAccount is not enough: the firmware only completes
// setup when the account is written *inside* a SETUP_START … SETUP_LEAVE bracket sent
// over the speaker's own WebSocket (port 8080, subprotocol "gabbo"). Until SETUP_LEAVE
// arrives the speaker keeps reporting source=SETUP, refuses to initialise its streaming
// services (no Spotify Connect) and keeps prompting for the app on every boot.
//
// Frame order, matching a capture of the official Bose app:
//
//	SETUP_START → SETUP_IDENTIFY_DEVICE_ENTER → sysLanguage → SETUP_ENTER
//	→ SETUP_IDENTIFY_DEVICE_LEAVE → name → setMargeAccount → SETUP_LEAVE
//	→ pushCustomerSupportInfoToMarge
//
// The session is run from the speaker itself (host 127.0.0.1). That matters: ReTouch's
// installer adds an iptables nat PREROUTING rule redirecting inbound :8080 to the web
// UI, so a session dialled from the LAN reaches ReTouch instead of the firmware.
// PREROUTING does not apply to loopback traffic, so on-box the firmware answers.
package setupsession

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Defaults for the session. Port and subprotocol are firmware properties; the language
// code and identify timeout are the values the Bose app sends during English setup.
const (
	wsPort           = 8080
	subprotocol      = "gabbo"
	languageEnglish  = 2
	identifyTimeout  = 300000 // ms; bracketed immediately by IDENTIFY_LEAVE, so not a real wait
	dialTimeout      = 8 * time.Second
	stepTimeout      = 8 * time.Second
	minSessionBudget = 5 * time.Second
)

// Plan is the input for Run. Name and BoseServer may be empty.
type Plan struct {
	Host       string // speaker host without port; "127.0.0.1" on-box
	DeviceID   string // from /info; every frame header carries it
	AccountID  string // marge account UUID to pair with (7 digits)
	AuthToken  string // userAuthToken; our own stub does not validate it
	Name       string // speaker name to (re)assert; empty = leave unchanged
	BoseServer string // marge base URL; adds <boseServer>/<updateServer>/<accountEmail>
	Port       int    // WebSocket port; 0 = the firmware's 8080 (tests override it)
}

// Run drives the whole state machine once and returns the first error it hits.
// It is safe to call on an already-paired speaker: the firmware accepts the bracket
// again and simply re-completes setup.
func Run(ctx context.Context, p Plan, log *slog.Logger) error {
	if p.DeviceID == "" {
		return errors.New("setup session: deviceID is required (read it from /info)")
	}
	if p.AccountID == "" {
		return errors.New("setup session: accountID is required")
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(discard{}, nil))
	}

	port := p.Port
	if port == 0 {
		port = wsPort
	}
	conn, err := wsDial(net.JoinHostPort(p.Host, strconv.Itoa(port)), subprotocol, dialTimeout)
	if err != nil {
		return fmt.Errorf("setup session: %w", err)
	}
	defer func() { _ = conn.close() }()

	s := &session{conn: conn, deviceID: p.DeviceID, log: log}

	steps := []struct {
		name  string
		route string
		verb  string
		body  string
	}{
		{"SETUP_START", "setup", "POST", `<setupState state="SETUP_START"/>`},
		{"SETUP_IDENTIFY_DEVICE_ENTER", "setup", "POST", fmt.Sprintf(`<setupState state="SETUP_IDENTIFY_DEVICE_ENTER" timeout="%d"/>`, identifyTimeout)},
		{"sysLanguage", "language", "POST", fmt.Sprintf(`<sysLanguage>%d</sysLanguage>`, languageEnglish)},
		{"SETUP_ENTER", "setup", "POST", `<setupState state="SETUP_ENTER"/>`},
		{"SETUP_IDENTIFY_DEVICE_LEAVE", "setup", "POST", `<setupState state="SETUP_IDENTIFY_DEVICE_LEAVE"/>`},
		{"name", "name", "POST", `<name>` + esc(p.Name) + `</name>`},
		{"setMargeAccount", "setMargeAccount", "POST", pairXML(p)},
		{"SETUP_LEAVE", "setup", "POST", `<setupState state="SETUP_LEAVE"/>`},
		{"pushCustomerSupportInfoToMarge", "pushCustomerSupportInfoToMarge", "GET", ""},
	}

	for _, st := range steps {
		if st.route == "name" && p.Name == "" {
			continue
		}
		if err := s.send(ctx, st.route, st.verb, st.body); err != nil {
			return fmt.Errorf("setup session: %s: %w", st.name, err)
		}
		log.Debug("setup session: step ok", "step", st.name)
	}
	log.Info("setup session: completed", "account", p.AccountID, "device", p.DeviceID)
	return nil
}

// pairXML builds the <PairDeviceWithAccount> body. The extended shape (boseServer,
// updateServer, accountEmail) is what the Bose app sends; the firmware persists those
// URLs, so passing them keeps the speaker pointed at ReTouch after setup completes.
func pairXML(p Plan) string {
	token := p.AuthToken
	if token == "" {
		token = "retouch"
	}
	var b strings.Builder
	b.WriteString(`<PairDeviceWithAccount><accountId>` + esc(p.AccountID) + `</accountId>`)
	b.WriteString(`<userAuthToken>` + esc(token) + `</userAuthToken>`)
	if p.BoseServer != "" {
		base := strings.TrimRight(p.BoseServer, "/")
		b.WriteString(`<boseServer>` + esc(base) + `</boseServer>`)
		b.WriteString(`<updateServer>` + esc(base+"/updates/soundtouch") + `</updateServer>`)
		// RFC 2606 reserves .invalid, so this address can never reach anyone.
		b.WriteString(`<accountEmail>local@retouch.invalid</accountEmail>`)
	}
	b.WriteString(`</PairDeviceWithAccount>`)
	return b.String()
}

type session struct {
	conn     *wsConn
	deviceID string
	reqID    int
	log      *slog.Logger
}

// send wraps body in the firmware's <msg> envelope, writes it, then reads frames until
// one acks this request. Pushed event frames and the SDK banner are not acks.
func (s *session) send(ctx context.Context, route, verb, body string) error {
	s.reqID++
	envelope := fmt.Sprintf(
		`<msg><header deviceID="%s" url="%s" method="%s"><request requestID="%d"/></header><body>%s</body></msg>`,
		esc(s.deviceID), esc(route), verb, s.reqID, body,
	)

	deadline := time.Now().Add(stepTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if time.Until(deadline) < minSessionBudget {
		return context.DeadlineExceeded
	}

	if err := s.conn.writeText(envelope, deadline); err != nil {
		return err
	}

	idNeedle := fmt.Sprintf(`requestID="%d"`, s.reqID)
	statusNeedle := "<status>/" + route + "</status>"
	urlNeedle := `url="` + route + `"`
	for {
		frame, err := s.conn.readText(deadline)
		if err != nil {
			return fmt.Errorf("await ack: %w", err)
		}
		if strings.Contains(frame, "<updates ") || strings.Contains(frame, "<SoundTouchSdkInfo") {
			continue // pushed notification, not our ack
		}
		if strings.Contains(strings.ToLower(frame), "<error") {
			return fmt.Errorf("device rejected: %s", strings.TrimSpace(frame))
		}
		if strings.Contains(frame, idNeedle) || strings.Contains(frame, statusNeedle) || strings.Contains(frame, urlNeedle) {
			return nil
		}
	}
}

// esc escapes text for both attribute and element contexts. Bodies are concatenated
// rather than marshalled because the fragments are already valid XML.
func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
