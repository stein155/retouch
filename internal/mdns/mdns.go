// Package mdns is a tiny multicast-DNS responder so the speaker can be reached at
// a friendly name like "keuken.local" instead of a bare IP. It advertises a single
// A record — <slug-of-speaker-name>.local → the speaker's LAN address — on the
// standard mDNS group (224.0.0.251:5353).
//
// It is deliberately small: stdlib only, IPv4 only, one name. It answers A/ANY
// queries for its name, announces itself on start and on rename, and sends a
// goodbye (TTL 0) on shutdown. Before claiming a name it probes for a conflict and
// falls back to "<name>-2", "<name>-3", … so two speakers never fight over one
// name. It coexists with the firmware's own mDNS (SO_REUSEADDR multicast bind);
// the two answer for different names.
package mdns

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ttlSeconds = 120
	typeA      = 1
	typeANY    = 255
	classIN    = 1
	flagQR     = 0x8000 // response
	flagAA     = 0x0400 // authoritative
	cacheFlush = 0x8000 // top bit of an answer's class
)

var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Responder advertises one A record and answers queries for it.
type Responder struct {
	ip   net.IP // 4-byte LAN address we advertise
	base string // slug of the speaker name, without ".local"
	log  *slog.Logger

	mu   sync.RWMutex
	host string // currently claimed fqdn, e.g. "keuken.local"

	conn   *net.UDPConn
	rename chan string
}

// New builds a responder for the given LAN ip and speaker name. The name is
// slugified to a DNS label; Run must be called to actually claim and serve it.
func New(ip, name string, log *slog.Logger) *Responder {
	base := slug(name)
	return &Responder{
		ip:     net.ParseIP(ip).To4(),
		base:   base,
		log:    log,
		host:   base + ".local",
		rename: make(chan string, 1),
	}
}

// Hostname returns the currently advertised name (e.g. "keuken.local").
func (r *Responder) Hostname() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.host
}

// SetName asks the responder to re-advertise under a new speaker name. It is
// safe to call from another goroutine; the change is applied by Run.
func (r *Responder) SetName(name string) {
	select {
	case r.rename <- name:
	default: // a rename is already queued; the latest wins on the next tick
	}
}

// Run claims the name and serves queries until ctx is cancelled.
func (r *Responder) Run(ctx context.Context) error {
	if r.ip == nil {
		return errNoIPv4
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return err
	}
	r.conn = conn
	defer func() { _ = conn.Close() }()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	// De-sync two equally-named speakers that boot at the same instant: stagger the
	// first probe by a small, per-IP-deterministic delay so one always claims the
	// plain name first and the other sees it and falls back to "<name>-2".
	select {
	case <-time.After(time.Duration(r.ip[3]) * 8 * time.Millisecond):
	case <-ctx.Done():
		return nil
	}

	r.claim(ctx, r.base)
	defer r.goodbye()

	buf := make([]byte, 1500)
	for {
		select {
		case name := <-r.rename:
			if b := slug(name); b != r.base {
				r.goodbye()
				r.base = b
				r.claim(ctx, b)
			}
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue // read timeout — loop to re-check rename
		}
		r.handleQuery(buf[:n], src)
	}
}

// claim probes for a free name (base, base-2, base-3, …), sets it, and announces.
func (r *Responder) claim(ctx context.Context, base string) {
	for i := 0; i < 20; i++ {
		cand := base
		if i > 0 {
			cand = base + "-" + strconv.Itoa(i+1)
		}
		fqdn := cand + ".local"
		if ctx.Err() != nil {
			return
		}
		if r.probe(fqdn) {
			r.mu.Lock()
			r.host = fqdn
			r.mu.Unlock()
			r.announce()
			r.log.Info("advertising", "host", fqdn, "ip", r.ip.String())
			return
		}
		r.log.Warn("name in use, trying next", "host", fqdn)
	}
}

// probe returns true if no other host already answers for fqdn with a different
// IP. It sends a few ANY queries and listens briefly for a conflicting answer.
func (r *Responder) probe(fqdn string) bool {
	q := question(fqdn)
	buf := make([]byte, 1500)
	deadline := time.Now().Add(900 * time.Millisecond)
	nextSend := time.Now()
	sent := 0
	for time.Now().Before(deadline) {
		if sent < 3 && !time.Now().Before(nextSend) {
			_, _ = r.conn.WriteToUDP(q, group)
			sent++
			nextSend = time.Now().Add(250 * time.Millisecond)
		}
		_ = r.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		n, _, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if ip, ok := answerAFor(buf[:n], fqdn); ok && !ip.Equal(r.ip) {
			return false // someone else owns this name
		}
	}
	return true
}

// handleQuery answers an incoming query if it asks for our name.
func (r *Responder) handleQuery(msg []byte, src *net.UDPAddr) {
	if len(msg) < 12 {
		return
	}
	if binary.BigEndian.Uint16(msg[2:4])&flagQR != 0 {
		return // a response, not a query
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	host := r.Hostname()
	off := 12
	for i := 0; i < qd; i++ {
		name, next, ok := readName(msg, off)
		if !ok || next+4 > len(msg) {
			return
		}
		qtype := binary.BigEndian.Uint16(msg[next : next+2])
		qclass := binary.BigEndian.Uint16(msg[next+2 : next+4])
		off = next + 4
		if (qtype == typeA || qtype == typeANY) && strings.EqualFold(name, host) {
			pkt := r.record(ttlSeconds)
			// Honour the unicast-response bit; otherwise answer the whole group.
			if qclass&0x8000 != 0 && src != nil {
				_, _ = r.conn.WriteToUDP(pkt, src)
			} else {
				_, _ = r.conn.WriteToUDP(pkt, group)
			}
		}
	}
}

// announce sends two unsolicited responses so caches pick up the name quickly.
func (r *Responder) announce() {
	pkt := r.record(ttlSeconds)
	for i := 0; i < 2; i++ {
		_, _ = r.conn.WriteToUDP(pkt, group)
		time.Sleep(120 * time.Millisecond)
	}
}

// goodbye announces the record with TTL 0 so resolvers drop it immediately.
func (r *Responder) goodbye() {
	if r.conn == nil {
		return
	}
	_, _ = r.conn.WriteToUDP(r.record(0), group)
}

// record builds an mDNS response carrying our single A record at the given TTL.
func (r *Responder) record(ttl uint32) []byte {
	host := r.Hostname()
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[2:4], flagQR|flagAA)
	binary.BigEndian.PutUint16(b[6:8], 1) // ANCOUNT
	b = append(b, encodeName(host)...)
	rr := make([]byte, 10)
	binary.BigEndian.PutUint16(rr[0:2], typeA)
	binary.BigEndian.PutUint16(rr[2:4], cacheFlush|classIN)
	binary.BigEndian.PutUint32(rr[4:8], ttl)
	binary.BigEndian.PutUint16(rr[8:10], 4) // RDLENGTH
	b = append(b, rr...)
	return append(b, r.ip...)
}

// question builds a standard mDNS ANY query for fqdn (used while probing).
func question(fqdn string) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:6], 1) // QDCOUNT
	b = append(b, encodeName(fqdn)...)
	q := make([]byte, 4)
	binary.BigEndian.PutUint16(q[0:2], typeANY)
	binary.BigEndian.PutUint16(q[2:4], classIN)
	return append(b, q...)
}

// answerAFor scans a DNS message and returns the IPv4 in the first A answer whose
// name matches fqdn (case-insensitive).
func answerAFor(msg []byte, fqdn string) (net.IP, bool) {
	if len(msg) < 12 {
		return nil, false
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	off := 12
	for i := 0; i < qd; i++ {
		_, next, ok := readName(msg, off)
		if !ok || next+4 > len(msg) {
			return nil, false
		}
		off = next + 4
	}
	for i := 0; i < an; i++ {
		name, next, ok := readName(msg, off)
		if !ok || next+10 > len(msg) {
			return nil, false
		}
		typ := binary.BigEndian.Uint16(msg[next : next+2])
		rdlen := int(binary.BigEndian.Uint16(msg[next+8 : next+10]))
		rdata := next + 10
		if rdata+rdlen > len(msg) {
			return nil, false
		}
		if typ == typeA && rdlen == 4 && strings.EqualFold(name, fqdn) {
			return net.IPv4(msg[rdata], msg[rdata+1], msg[rdata+2], msg[rdata+3]).To4(), true
		}
		off = rdata + rdlen
	}
	return nil, false
}

// encodeName encodes a dotted name as length-prefixed DNS labels ending in a zero
// byte. No compression — our names are short.
func encodeName(name string) []byte {
	var b []byte
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" {
			continue
		}
		if len(label) > 63 {
			label = label[:63]
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}

// readName decodes a DNS name at off, following compression pointers. It returns
// the name, the offset just past the name in the record stream, and ok.
func readName(msg []byte, off int) (string, int, bool) {
	var labels []string
	next := -1
	for hops := 0; hops < 128; hops++ {
		if off < 0 || off >= len(msg) {
			return "", 0, false
		}
		c := msg[off]
		switch {
		case c == 0:
			off++
			if next < 0 {
				next = off
			}
			return strings.Join(labels, "."), next, true
		case c&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return "", 0, false
			}
			if next < 0 {
				next = off + 2
			}
			off = int(c&0x3F)<<8 | int(msg[off+1])
		default:
			l := int(c)
			if off+1+l > len(msg) {
				return "", 0, false
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
	return "", 0, false
}

// slug turns a speaker name into a single safe DNS label: lowercase ASCII
// letters/digits, spaces and underscores become single dashes, anything else is
// dropped. Empty input falls back to "retouch".
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			dash = false
		case c == ' ' || c == '-' || c == '_':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "retouch"
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

type mdnsError string

func (e mdnsError) Error() string { return string(e) }

const errNoIPv4 = mdnsError("mdns: no IPv4 address to advertise")
