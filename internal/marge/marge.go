// Package marge is the minimal local cloud-pairing stub that keeps the speaker's
// NATIVE music sources alive after Bose shut the cloud down. The speaker resolves
// and streams TuneIn itself (via the live TuneIn service); this stub only has to
// keep the speaker "associated" (logged in) and return a service registry that lists
// TUNEIN / INTERNET_RADIO so the firmware keeps those workers enabled.
//
// Rather than reimplement the Bose cloud's protobuf-backed account documents from
// scratch (the firmware decodes /full into a protobuf with required fields and
// aborts the whole sync on any mismatch), the stub REPLAYS responses that a known
// working local cloud already served to this exact speaker — captured once and
// embedded here. The only edit applied on the wire is rewriting the captured
// server's hostname to wherever this stub is reached, so the BMX service base URLs
// resolve back to us.
//
// Reached by the speaker over plain HTTP: the on-speaker installer points the speaker's
// SoundTouchSdkPrivateCfg.xml (margeServerUrl / statsServerUrl / swUpdateUrl /
// bmxRegistryUrl) at this server. No TLS, no CA, no /etc/hosts redirect needed.
package marge

import (
	"crypto/sha256"
	"embed"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

//go:embed data
var data embed.FS

// The embedded responses were captured from a working local cloud and have every
// speaker- and deployment-specific value replaced with a placeholder token (so the
// repo carries no real hostnames, account IDs or serials). New rewrites these to
// the running stub's base and the live speaker's own identity (from /info), so the
// BMX registry points back at us and the account documents describe this speaker.
const (
	tokBase      = "__STL_BASE__"    // → marge base URL (this stub)
	tokAccount   = "__STL_ACCOUNT__" // → margeAccountUUID
	tokDevice    = "__STL_DEVICE__"  // → deviceID
	tokIP        = "__STL_IP__"      // → LAN address
	tokName      = "__STL_NAME__"    // → speaker name
	tokSerialSCM = "__STL_SERIAL_SCM__"
	tokSerialPkg = "__STL_SERIAL_PKG__"
)

// Content-Type values the firmware expects per endpoint (mirrors the captured
// responses' own headers).
const (
	ctJSON      = "application/json"
	ctStreamV12 = "application/vnd.bose.streaming-v1.2+xml"
	ctStreamV11 = "application/vnd.bose.streaming-v1.1+xml"
)

// doc is one pre-rendered response: body, Content-Type and a stable ETag.
type doc struct {
	ct   string
	body []byte
	etag string
}

// Server answers the speaker's cloud calls by replaying captured responses.
type Server struct {
	log        *slog.Logger
	token      string // association token handed to the speaker during pairing
	tunein     TuneIn
	nowPlaying NowPlayingSource // live track for the display; nil disables it

	recMu  sync.Mutex
	recent []string // ring buffer of recent speaker requests, for /debug/requests

	// repoll gates the display-track injection: the station whose playback doc was
	// last fetched, and how many times in a row. Only one station plays at a time,
	// so tracking the last id is enough to spot a re-fetch (see sawRepoll).
	repollMu    sync.Mutex
	lastStation string
	lastCount   int

	registry        doc
	availability    doc
	sourceProviders doc
	accountFull     doc
	accountSources  doc
	accountDevices  doc
	presets         *presets
}

// New builds the stub. base is the URL the speaker reaches this server at (e.g.
// "http://127.0.0.1:9080"); info carries the live speaker's identity (from /info)
// used to personalise the replayed account documents. info may be nil for an
// off-speaker dry run, in which case neutral placeholders are kept.
func New(base string, info *speaker.Info, presetsPath string, nativePresets []PresetSeed, tc TuneIn, log *slog.Logger) (*Server, error) {
	base = strings.TrimRight(base, "/")
	if info == nil {
		info = &speaker.Info{}
	}
	repl := strings.NewReplacer(
		tokBase, base,
		tokAccount, xmlText(info.Account),
		tokDevice, xmlText(info.DeviceID),
		tokIP, xmlText(info.IP),
		tokName, xmlText(info.Name),
		tokSerialSCM, xmlText(info.SerialSCM),
		tokSerialPkg, xmlText(info.SerialPkg),
	)
	s := &Server{log: log, token: "stlocal-assoc-token", tunein: tc}
	load := func(file, ct string) (doc, error) {
		b, err := data.ReadFile("data/" + file)
		if err != nil {
			return doc{}, fmt.Errorf("load %s: %w", file, err)
		}
		b = []byte(repl.Replace(string(b)))
		sum := sha256.Sum256(b)
		return doc{ct: ct, body: b, etag: fmt.Sprintf("%x", sum[:8])}, nil
	}
	var err error
	for _, l := range []struct {
		dst  *doc
		file string
		ct   string
	}{
		{&s.registry, "registry.json", ctJSON},
		{&s.availability, "availability.json", ctJSON},
		{&s.sourceProviders, "sourceproviders.xml", ctStreamV12},
		{&s.accountFull, "account_full.xml", ctStreamV12},
		{&s.accountSources, "account_sources.xml", ctStreamV11},
		{&s.accountDevices, "account_devices.xml", ctStreamV11},
	} {
		if *l.dst, err = load(l.file, l.ct); err != nil {
			return nil, err
		}
	}
	if info.Software != "" {
		s.accountFull = rewriteFirmware(s.accountFull, info.Software)
		s.accountDevices = rewriteFirmware(s.accountDevices, info.Software)
	}
	// Presets are dynamic (write-through + persisted), seeded from the capture.
	seed, err := load("presets_all.xml", ctStreamV11)
	if err != nil {
		return nil, err
	}
	s.presets = newPresets(seed.body, presetsPath)
	s.presets.seedNative(nativePresets)
	return s, nil
}

// SetNowPlaying attaches the live-track source used to drive the current track
// onto the speaker's display. Optional; nil leaves the display showing just the
// station name, as before. Call before Handler().
func (s *Server) SetNowPlaying(np NowPlayingSource) { s.nowPlaying = np }

// sawRepoll records a playback-doc fetch for stationID and reports whether the
// firmware has fetched THIS station's doc more than once in a row — i.e. it polls
// for updates. The first fetch of a station returns false (so the display keeps
// the station name and never freezes on a select-time track); a repeat returns
// true. A new station resets the count.
func (s *Server) sawRepoll(stationID string) bool {
	s.repollMu.Lock()
	defer s.repollMu.Unlock()
	if stationID == s.lastStation {
		s.lastCount++
	} else {
		s.lastStation = stationID
		s.lastCount = 1
	}
	return s.lastCount > 1
}

func rewriteFirmware(d doc, software string) doc {
	body := string(d.body)
	body = rewriteElementText(body, "firmware-version", software)
	body = rewriteElementText(body, "firmwareVersion", software)
	d.body = []byte(body)
	sum := sha256.Sum256(d.body)
	d.etag = fmt.Sprintf("%x", sum[:8])
	return d
}

func rewriteElementText(s, elem, text string) string {
	open := "<" + elem + ">"
	close := "</" + elem + ">"
	var b strings.Builder
	for {
		start := strings.Index(s, open)
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		valueStart := start + len(open)
		end := strings.Index(s[valueStart:], close)
		if end < 0 {
			b.WriteString(s)
			return b.String()
		}
		end += valueStart
		b.WriteString(s[:valueStart])
		if strings.TrimSpace(s[valueStart:end]) == "" {
			b.WriteString(s[valueStart:end])
		} else {
			b.WriteString(xmlText(text))
		}
		s = s[end:]
	}
}

// Handler returns the HTTP mux for the Bose cloud endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/debug/requests", s.debugRequests)
	mux.HandleFunc("/", s.route)
	return mux
}

// debugRequests returns the recent speaker-request log (for on-speaker diagnosis: the speaker
// reaches marge on loopback, so this is the only window into what it asks for).
func (s *Server) debugRequests(w http.ResponseWriter, _ *http.Request) {
	s.recMu.Lock()
	out := strings.Join(s.recent, "\n")
	s.recMu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(out))
}

func (s *Server) record(method, path string) {
	s.recMu.Lock()
	s.recent = append(s.recent, time.Now().UTC().Format("15:04:05")+" "+method+" "+path)
	if len(s.recent) > 100 {
		s.recent = s.recent[len(s.recent)-100:]
	}
	s.recMu.Unlock()
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	s.record(r.Method, p)
	s.log.Debug("speaker request", "method", r.Method, "path", p, "if-none-match", r.Header.Get("If-None-Match"))
	switch {
	case p == "/v1/auth":
		// /speaker app_key validation. Firmware only needs HTTP 200 here.
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(p, "/bmx/tunein"):
		s.bmxTunein(w, r)
	case strings.HasPrefix(p, "/bmx/registry/") && strings.Contains(p, "servicesAvailability"):
		s.serve(w, r, s.availability)
	case strings.HasPrefix(p, "/bmx/registry/"):
		s.serve(w, r, s.registry)
	case strings.HasPrefix(p, "/streaming/sourceproviders"):
		s.serve(w, r, s.sourceProviders)
	case strings.HasPrefix(p, "/streaming/account/") && strings.HasSuffix(p, "/full"):
		s.serveFull(w, r)
	case strings.HasPrefix(p, "/streaming/account/") && strings.HasSuffix(p, "/sources"):
		s.serve(w, r, s.accountSources)
	case strings.HasPrefix(p, "/streaming/account/") && strings.HasSuffix(p, "/devices"):
		s.serve(w, r, s.accountDevices)
	case strings.HasPrefix(p, "/streaming/account/") && strings.HasSuffix(p, "/presets/all"):
		s.servePresets(w, r)
	case strings.HasPrefix(p, "/streaming/account/") && isPresetStore(p) && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		s.updatePreset(w, r)
	case strings.HasPrefix(p, "/streaming/account/") && strings.Contains(p, "/device") && r.Method == http.MethodPost:
		s.addDevice(w)
	case strings.HasPrefix(p, "/streaming/account") || strings.HasPrefix(p, "/streaming/auth"):
		// Any other associated-account read: replaying /full keeps the speaker logged in.
		s.serve(w, r, s.accountFull)
	case strings.HasPrefix(p, "/streaming/support"):
		s.ok(w)
	default:
		// Generic 200 so the speaker never enters a cloud-down retry loop.
		s.log.Debug("marge catchall", "method", r.Method, "path", p)
		s.xml(w, `<?xml version="1.0" encoding="UTF-8"?><ack/>`)
	}
}

func (s *Server) serveFull(w http.ResponseWriter, r *http.Request) {
	body := splicePresets(s.accountFull.body, s.presets.renderInner())
	// The spliced body carries per-render preset timestamps, so hashing it would
	// churn the ETag every request and the firmware's conditional GET would never
	// hit. The static /full doc is fixed at construction and the only varying part
	// is the presets, so a stable tag = full-doc etag + presets seq.
	etag := s.accountFull.etag + "-" + s.presets.tag()
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", s.accountFull.ct)
	w.Header()["ETag"] = []string{etag}
	_, _ = w.Write(body)
}

func splicePresets(body []byte, inner string) []byte {
	s := string(body)
	start := strings.Index(s, "<presets>")
	end := strings.Index(s, "</presets>")
	if start < 0 || end < start {
		return body
	}
	start += len("<presets>")
	return []byte(s[:start] + inner + s[end:])
}

// serve writes a captured document, honouring If-None-Match with a 304. The ETag
// header is written via the raw header map: net/http would canonicalise Set()'s
// key to "Etag", which the firmware does not match against its cached value.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, d doc) {
	if r.Header.Get("If-None-Match") == d.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", d.ct)
	w.Header()["ETag"] = []string{d.etag}
	_, _ = w.Write(d.body)
}

// servePresets renders the six buttons (dynamic) with ETag + 304.
func (s *Server) servePresets(w http.ResponseWriter, r *http.Request) {
	body, etag := s.presets.render()
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", ctStreamV11)
	w.Header()["ETag"] = []string{etag}
	_, _ = w.Write(body)
}

// isPresetStore matches the speaker's long-press store paths, which carry both a
// device segment and a trailing preset button number, e.g.
// /streaming/account/<a>/device/<d>/presets/<n> or .../preset/<n>.
func isPresetStore(p string) bool {
	if !strings.Contains(p, "/device") {
		return false
	}
	i := strings.LastIndex(p, "/preset")
	if i < 0 {
		return false
	}
	tail := p[i:]
	return strings.HasPrefix(tail, "/preset/") || strings.HasPrefix(tail, "/presets/")
}

// updatePreset writes a long-press preset store through to the dynamic store. The
// firmware sends the name as <name> (the mobile app as <username>); location is the
// TUNEIN station path the speaker will resolve itself.
func (s *Server) updatePreset(w http.ResponseWriter, r *http.Request) {
	btn := presetButton(r.URL.Path)
	// Bound the body: a preset store is a few hundred bytes; cap it so a malformed
	// or hostile request can't drive a large allocation on the speaker.
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var in struct {
		Name     string `xml:"name"`
		Username string `xml:"username"`
		SourceID string `xml:"sourceid"`
		Location string `xml:"location"`
		Type     string `xml:"contentItemType"`
		Art      string `xml:"containerArt"`
	}
	if err := xml.NewDecoder(r.Body).Decode(&in); err != nil || btn < 1 {
		s.log.Warn("updatePreset: bad body/button", "path", r.URL.Path, "err", err)
		http.Error(w, "bad preset", http.StatusBadRequest)
		return
	}
	name := in.Name
	if name == "" {
		name = in.Username
	}
	s.presets.set(btn, presetSlot{Name: name, Location: in.Location, Type: in.Type, Art: in.Art})
	s.log.Info("preset stored", "button", btn, "name", name, "location", in.Location)
	body, etag := s.presets.render()
	w.Header().Set("Content-Type", ctStreamV11)
	w.Header()["ETag"] = []string{etag}
	_, _ = w.Write(body)
}

// MirrorPreset keeps the cloud-facing preset list aligned with writes made through
// the web UI's direct speaker API path.
func (s *Server) MirrorPreset(slot int, name, location, art string) {
	s.presets.set(slot, presetSlot{Name: name, Location: location, Type: "stationurl", Art: art})
}

// RemovePreset mirrors a direct speaker preset deletion into the cloud-facing list.
func (s *Server) RemovePreset(slot int) {
	s.presets.remove(slot)
}

// presetButton extracts the trailing button number from a store path; 0 if absent.
func presetButton(p string) int {
	last := p[strings.LastIndex(p, "/")+1:]
	n := 0
	for _, c := range last {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 6 {
		return 0
	}
	return n
}

// addDevice answers the AddDevice sync the speaker triggers after POST
// :8090/setMargeAccount: an adddeviceresponse carrying a margetoken moves the speaker
// to MargeStateAssociated (logged in).
func (s *Server) addDevice(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ctStreamV12)
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><response status="OK"><adddeviceresponse><margetoken>%s</margetoken></adddeviceresponse></response>`, s.token)))
}

func (s *Server) ok(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ctStreamV12)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><response status="OK"/>`))
}

func (s *Server) xml(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// xmlText escapes a substitution value so it is safe in both element text and
// double-quoted attribute contexts in the replayed documents.
func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
