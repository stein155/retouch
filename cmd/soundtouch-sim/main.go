// Command soundtouch-sim runs the SoundTouch speaker simulator on the real speaker
// ports (REST API 8090, diagnostic CLI 17000), so the firmware can be pointed at it
// with -speaker-host 127.0.0.1 for manual end-to-end testing without a physical
// speaker. Identity fields can be overridden via flags.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/stein155/retouch/internal/sim"
)

func main() {
	api := flag.String("api", ":8090", "SoundTouch REST API listen address")
	cli := flag.String("cli", ":17000", "diagnostic CLI listen address")
	name := flag.String("name", "", "override speaker name")
	id := flag.String("device-id", "", "override device id")
	ip := flag.String("ip", "", "override reported LAN ip")
	flag.Parse()

	sp := sim.New()
	if *name != "" {
		sp.Name = *name
	}
	if *id != "" {
		sp.DeviceID = *id
	}
	if *ip != "" {
		sp.IP = *ip
	}

	ln, err := net.Listen("tcp", *cli)
	if err != nil {
		log.Fatalf("listen CLI %s: %v", *cli, err)
	}
	go sp.ServeCLI(ln)

	log.Printf("soundtouch-sim: api %s, cli %s, device %s (%s)", *api, *cli, sp.Name, sp.DeviceID)
	log.Fatal(http.ListenAndServe(*api, sp.Handler()))
}
