package speaker

import (
	"encoding/xml"
	"testing"
)

var (
	master = Member{DeviceID: "F4E11E3B013F", IP: "192.168.1.10"}
	slaveA = Member{DeviceID: "A0F6FD123456", IP: "192.168.1.11"}
	slaveB = Member{DeviceID: "B1E7AE789ABC", IP: "192.168.1.12"}
)

// TestSetZoneBody pins the exact /setZone wire format the SoundTouch firmware
// expects: master id + senderIPAddress on the <zone>, then ONLY the slaves as
// members (the master is named by the attribute, not repeated as a member —
// matching libsoundtouch / Home Assistant).
func TestSetZoneBody(t *testing.T) {
	got := setZoneBody(master, []Member{slaveA, slaveB})
	want := `<zone master="F4E11E3B013F" senderIPAddress="192.168.1.10">` +
		`<member ipaddress="192.168.1.11">A0F6FD123456</member>` +
		`<member ipaddress="192.168.1.12">B1E7AE789ABC</member>` +
		`</zone>`
	if got != want {
		t.Errorf("setZoneBody:\n got %s\nwant %s", got, want)
	}
}

// TestZoneBody pins /addZoneSlave & /removeZoneSlave: master id only (no
// senderIPAddress, no master member), then just the affected members.
func TestZoneBody(t *testing.T) {
	got := zoneBody(master, []Member{slaveA})
	want := `<zone master="F4E11E3B013F">` +
		`<member ipaddress="192.168.1.11">A0F6FD123456</member>` +
		`</zone>`
	if got != want {
		t.Errorf("zoneBody:\n got %s\nwant %s", got, want)
	}
}

// TestZoneBodyEscaping makes sure odd values can't break the XML.
func TestZoneBodyEscaping(t *testing.T) {
	got := zoneBody(Member{DeviceID: `a&b"`, IP: "1.2.3.4"}, nil)
	if want := `<zone master="a&amp;b&quot;"></zone>`; got != want {
		t.Errorf("escaping:\n got %s\nwant %s", got, want)
	}
}

// roundTrip the getZone parser against a representative firmware response.
func TestGetZoneParse(t *testing.T) {
	body := []byte(`<zone master="F4E11E3B013F" senderIPAddress="192.168.1.10">` +
		`<member ipaddress="192.168.1.10">F4E11E3B013F</member>` +
		`<member ipaddress="192.168.1.11">A0F6FD123456</member>` +
		`</zone>`)
	var x struct {
		Master  string `xml:"master,attr"`
		Members []struct {
			IP       string `xml:"ipaddress,attr"`
			DeviceID string `xml:",chardata"`
		} `xml:"member"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		t.Fatal(err)
	}
	if x.Master != "F4E11E3B013F" || len(x.Members) != 2 {
		t.Fatalf("unexpected parse: %+v", x)
	}
	if x.Members[1].IP != "192.168.1.11" || x.Members[1].DeviceID != "A0F6FD123456" {
		t.Errorf("slave member parsed wrong: %+v", x.Members[1])
	}
}
