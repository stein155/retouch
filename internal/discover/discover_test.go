package discover

import (
	"context"
	"testing"
)

func TestSlash24(t *testing.T) {
	tests := []struct {
		name       string
		ip         string
		wantPrefix string
		wantOK     bool
	}{
		{"typical", "192.168.2.27", "192.168.2.", true},
		{"low octets", "10.0.0.1", "10.0.0.", true},
		{"high octet", "192.168.255.254", "192.168.255.", true},
		{"zero last", "172.16.5.0", "172.16.5.", true},
		{"empty", "", "", false},
		{"garbage", "not-an-ip", "", false},
		{"ipv6", "fe80::1", "", false},
		{"trailing junk", "192.168.2.27x", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrefix, gotOK := slash24(tt.ip)
			if gotOK != tt.wantOK || gotPrefix != tt.wantPrefix {
				t.Errorf("slash24(%q) = (%q, %v), want (%q, %v)",
					tt.ip, gotPrefix, gotOK, tt.wantPrefix, tt.wantOK)
			}
		})
	}
}

// slash24 must produce a prefix that, when an octet is appended, parses back to a
// valid address in the same /24 — this is exactly how Scan builds candidate IPs.
func TestSlash24PrefixComposes(t *testing.T) {
	prefix, ok := slash24("192.168.2.27")
	if !ok {
		t.Fatal("slash24 unexpectedly failed")
	}
	for _, n := range []string{"1", "27", "254"} {
		ip := prefix + n
		if _, ok := slash24(ip); !ok {
			t.Errorf("composed ip %q does not re-parse as IPv4", ip)
		}
	}
}

func TestErrNoSubnet(t *testing.T) {
	err := errNoSubnet{ip: "bad"}
	if got, want := err.Error(), "discover: cannot derive subnet from ip bad"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// Scan must fail fast (not sweep the LAN) when selfIP has no derivable /24.
func TestScanRejectsBadSubnet(t *testing.T) {
	got, err := Scan(context.Background(), "not-an-ip", "DEAD")
	if err == nil {
		t.Fatal("expected error for un-parseable selfIP, got nil")
	}
	if got != nil {
		t.Errorf("expected nil result on error, got %+v", got)
	}
	if _, ok := err.(errNoSubnet); !ok {
		t.Errorf("expected errNoSubnet, got %T: %v", err, err)
	}
}
