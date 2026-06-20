// Package discover finds other ReTouch speakers on the local network so they can
// be grouped into a multiroom zone. It mirrors what install.sh does: sweep the
// local /24 and ask each address for /info (port 8090); anything that answers
// with a deviceID is a SoundTouch. We then keep ONLY the ones that also have
// ReTouch running — by probing ReTouch's own API on :8080 — so the list shows
// only speakers we manage, not every Bose on the network. No mDNS, no cloud.
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// Speaker is one discovered ReTouch speaker on the LAN.
type Speaker struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Model    string `json:"model"`
	IP       string `json:"ip"`
}

const (
	probeTimeout = 700 * time.Millisecond // per-host probe budget
	parallelism  = 24                     // concurrent probes; keeps the sweep quick without flooding
	retouchPort  = 8080                   // the uniform LAN port ReTouch is exposed on
)

// Scan sweeps the /24 around selfIP and returns every speaker that answers as a
// SoundTouch AND has ReTouch running, excluding the speaker at selfIP (and itself
// by deviceID). selfIP is this speaker's own LAN address (from /info). Probes run
// in parallel with a short per-host timeout so a full sweep finishes in a couple
// of seconds.
func Scan(ctx context.Context, selfIP, selfDeviceID string) ([]Speaker, error) {
	prefix, ok := slash24(selfIP)
	if !ok {
		return nil, errNoSubnet{ip: selfIP}
	}

	var (
		mu    sync.Mutex
		found []Speaker
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, parallelism)
	for n := 1; n <= 254; n++ {
		ip := prefix + strconv.Itoa(n)
		if ip == selfIP {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			sp, ok := probe(ctx, ip)
			if !ok || sp.DeviceID == selfDeviceID || !retouchRunning(ctx, ip) {
				return
			}
			mu.Lock()
			found = append(found, sp)
			mu.Unlock()
		}(ip)
	}
	wg.Wait()

	sort.Slice(found, func(i, j int) bool {
		if found[i].Name != found[j].Name {
			return found[i].Name < found[j].Name
		}
		return found[i].DeviceID < found[j].DeviceID
	})
	return found, nil
}

// probe asks one address for /info with a short timeout. A non-speaker (or an
// unused address) just times out and is skipped.
func probe(ctx context.Context, ip string) (Speaker, bool) {
	c, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	info, err := speaker.New(ip).Info(c)
	if err != nil || info.DeviceID == "" {
		return Speaker{}, false
	}
	return Speaker{DeviceID: info.DeviceID, Name: info.Name, Model: info.Type, IP: ip}, true
}

// retouchRunning reports whether ReTouch answers on its LAN port at ip. It hits
// ReTouch's own /api/version (200 + a version string), so a plain SoundTouch
// without ReTouch is left out of the list.
func retouchRunning(ctx context.Context, ip string) bool {
	c, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	url := fmt.Sprintf("http://%s:%d/api/version", ip, retouchPort)
	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&v); err != nil {
		return false
	}
	return v.Version != ""
}

// slash24 returns the "a.b.c." prefix of a dotted IPv4 address.
func slash24(ip string) (string, bool) {
	v4 := net.ParseIP(ip).To4()
	if v4 == nil {
		return "", false
	}
	return strconv.Itoa(int(v4[0])) + "." + strconv.Itoa(int(v4[1])) + "." + strconv.Itoa(int(v4[2])) + ".", true
}

type errNoSubnet struct{ ip string }

func (e errNoSubnet) Error() string { return "discover: cannot derive subnet from ip " + e.ip }
