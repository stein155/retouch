// Command retouch is a tiny on-speaker agent for Bose SoundTouch speakers after the
// Bose cloud shutdown. It keeps the speaker's NATIVE music sources alive with a minimal
// local pairing stub (so TUNEIN / INTERNET_RADIO keep working) and serves a web UI to
// search TuneIn, manage the 6 presets, play, and set volume. No cloud, no desktop app.
//
// The logic lives in package app so the same code can be composed into other
// single-binary builds via app.RegisterService.
package main

import "github.com/stein155/retouch/app"

var version = "dev"

func main() {
	app.SetVersion(version)
	app.Run()
}
