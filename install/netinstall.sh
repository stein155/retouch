#!/bin/sh
# ReTouch on-speaker network installer. Fetched + run by the :17000 boot command (see
# install.sh). It runs ON the speaker as root, so it can both install the agent and
# point the speaker's cloud URLs at it — no SSH, no USB stick.
#
# Install AND update: it records the installed release tag in .version and compares
# it to the latest GitHub release each run. Re-running install.sh therefore upgrades
# the speaker to the newest release in place (and restarts the agent); if it is
# already on the latest tag it just re-asserts the config and makes sure the agent
# is running.
#
# NOTE: scaffold — verify on hardware before relying on it. Assumes the speaker's
# busybox curl can reach GitHub over TLS (true on the SoundTouch 10 / fw27).
set -u

REPO=stein155/retouch
PIN_TAG="${RETOUCH_TARGET_TAG:-}" # set e.g. "v0.1.0" to pin; empty = latest
HOME_DIR=/mnt/nv/retouch
BIN=$HOME_DIR/retouch
VERSION=$HOME_DIR/.version      # installed release tag
GAVEUP=$HOME_DIR/.gaveup
ATTEMPTS=$HOME_DIR/.attempts
LOCK=/tmp/retouch-install.lock
LOG=/tmp/retouch-install.log
MAX_ATTEMPTS=5

# Where the speaker reaches the on-speaker pairing stub and web UI.
MARGE_BASE=http://127.0.0.1:9080
WEB_LISTEN=:8000
MARGE_LISTEN=:9080
CFG=/opt/Bose/etc/SoundTouchSdkPrivateCfg.xml
START=$HOME_DIR/start.sh

log() { echo "[retouch] $*" >>"$LOG" 2>&1; }
# giveup records the TARGET tag it gave up on (not just an empty marker) so a later
# run can tell "gave up on this exact release" from "a newer release is out, retry".
giveup() { echo "${TAG:-}" >"$GAVEUP"; log "$*; giving up (target ${TAG:-?})"; exit 0; }

LAUNCH="$BIN -speaker-host 127.0.0.1 -listen $WEB_LISTEN -listen-marge $MARGE_LISTEN -marge-base $MARGE_BASE -presets $HOME_DIR/presets.json"

start_agent() {
	if pidof retouch >/dev/null 2>&1; then
		return 0
	fi
	[ -x "$START" ] && "$START" >/tmp/retouch-start.log 2>&1 &
}

# restart_agent stops a running agent (e.g. the old version started at boot) and
# launches the freshly installed binary. Used after an install/update.
restart_agent() {
	pid=$(pidof retouch 2>/dev/null) && [ -n "$pid" ] && { kill $pid 2>/dev/null; sleep 1; }
	[ -x "$START" ] && "$START" >/tmp/retouch-start.log 2>&1 &
}

# write_start_script writes the boot launcher. It binds the web UI on $WEB_LISTEN and
# then makes a BEST-EFFORT attempt to expose it on exactly one uniform port, :8080,
# while hiding the raw $WEB_LISTEN port from the LAN — WITHOUT touching Bose's own setup
# servers. If the rules can't be installed, the UI is still served on $WEB_LISTEN, so it
# is never lost. iptables is volatile, so this re-applies on every boot.
write_start_script() {
	cat > "$START" <<STARTSCRIPT
#!/bin/sh

LOG=/tmp/retouch.log
APP_PORT=${WEB_LISTEN#:}

log() { echo "[retouch-start] \$*" >>"\$LOG" 2>&1; }

# expose_8080 makes ReTouch reachable on EXACTLY ONE port: :8080 (the uniform port that
# works on every speaker — including the dual-processor SoundTouch 20/30, where LAN :80
# is owned by a second processor and can't be redirected, and :8000 is firewalled). It
# redirects inbound :8080 to the app port, and then DROPS direct LAN access to the app
# port itself so the UI is NOT also exposed on :8000. Loopback access to the app port is
# preserved (the speaker/agent use it locally). The raw table runs before nat, so the
# drop only hits direct :8000 traffic, never the :8080-redirected flow. Best-effort and
# reversible (flushes on reboot; if the rules can't be set, the UI stays on :APP_PORT).
expose_8080() {
	command -v iptables >/dev/null 2>&1 || { log "no iptables; UI on :\$APP_PORT only"; return 0; }
	# clear any :80 redirect left by an older version (we expose ONLY :8080 now)
	while iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "\$APP_PORT" 2>/dev/null; do :; done
	iptables -t nat -D PREROUTING -p tcp --dport 8080 -j REDIRECT --to-ports "\$APP_PORT" 2>/dev/null
	if iptables -t nat -I PREROUTING 1 -p tcp --dport 8080 -j REDIRECT --to-ports "\$APP_PORT" 2>>"\$LOG"; then
		log "redirected :8080 -> :\$APP_PORT"
	else
		log "could not redirect :8080 (UI still on :\$APP_PORT)"
	fi
	iptables -t raw -D PREROUTING ! -i lo -p tcp --dport "\$APP_PORT" -j DROP 2>/dev/null
	if iptables -t raw -I PREROUTING 1 ! -i lo -p tcp --dport "\$APP_PORT" -j DROP 2>>"\$LOG"; then
		log "hid direct LAN access to :\$APP_PORT (loopback kept)"
	else
		log "could not hide :\$APP_PORT; it may stay reachable directly"
	fi
}

expose_8080
$LAUNCH >>"\$LOG" 2>&1 &
STARTSCRIPT
	chmod 0755 "$START" 2>/dev/null
}

# write_rc_local installs the NAND autostart line, which runs the boot launcher.
write_rc_local() {
	write_start_script
	cat > /mnt/nv/rc.local <<RC
#!/bin/sh
$START >/tmp/retouch-start.log 2>&1 &
RC
	chmod 0755 /mnt/nv/rc.local 2>/dev/null
}

# redirect_cloud rewrites the four service URLs in SoundTouchSdkPrivateCfg.xml to
# MARGE_BASE, keeping a one-time .original backup. Idempotent: re-running only
# rewrites if the file does not already point at us. Requires a read-write rootfs.
redirect_cloud() {
	[ -f "$CFG" ] || { log "no $CFG (firmware layout differs) — skipping URL redirect"; return 1; }
	mount / -o rw,remount 2>>"$LOG" || mount -o remount,rw / 2>>"$LOG" || { log "could not remount / rw"; return 1; }
	[ -f "$CFG.original" ] || cp "$CFG" "$CFG.original"
	if grep -q "$MARGE_BASE" "$CFG" 2>/dev/null; then log "cloud already redirected"; mount / -o ro,remount 2>/dev/null; return 0; fi
	sed \
		-e "s#<margeServerUrl>[^<]*</margeServerUrl>#<margeServerUrl>$MARGE_BASE</margeServerUrl>#" \
		-e "s#<statsServerUrl>[^<]*</statsServerUrl>#<statsServerUrl>$MARGE_BASE</statsServerUrl>#" \
		-e "s#<swUpdateUrl>[^<]*</swUpdateUrl>#<swUpdateUrl>$MARGE_BASE/updates/soundtouch</swUpdateUrl>#" \
		-e "s#<bmxRegistryUrl>[^<]*</bmxRegistryUrl>#<bmxRegistryUrl>$MARGE_BASE/bmx/registry/v1/services</bmxRegistryUrl>#" \
		"$CFG.original" > "$CFG.new" && mv "$CFG.new" "$CFG"
	log "redirected cloud URLs -> $MARGE_BASE (backup at $CFG.original)"
	mount / -o ro,remount 2>/dev/null
}

mkdir "$LOCK" 2>/dev/null || { log "locked"; exit 0; }
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

mkdir -p "$HOME_DIR" 2>/dev/null

# Resolve the target release tag (pinned, else the latest GitHub release). At boot
# the injected run can fire BEFORE the network/DNS is ready, so the latest-release
# lookup returns nothing on the first try; retry for up to ~2 min rather than giving
# up immediately — otherwise the speaker silently keeps the old binary (the injection
# fires only once per boot) and an install.sh run waits forever for the new version.
TAG="$PIN_TAG"
if [ -z "$TAG" ]; then
	t=0
	while [ "$t" -lt 30 ]; do
		TAG=$(curl -fsSL https://api.github.com/repos/$REPO/releases/latest \
			| sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
		[ -n "$TAG" ] && break
		log "GitHub not reachable yet (boot network warming up); retry $((t + 1))/30"
		t=$((t + 1)); sleep 4
	done
fi
INSTALLED=$(cat "$VERSION" 2>/dev/null || echo "")

# Already current (or GitHub unreachable but a binary exists): just re-assert the
# config + autostart, no download. Clear the attempt counter so a later update run
# starts fresh.
if [ -x "$BIN" ] && { [ -z "$TAG" ] || [ "$TAG" = "$INSTALLED" ]; }; then
	[ -z "$TAG" ] && log "could not reach GitHub; keeping installed ${INSTALLED:-?}" || log "already up to date ($INSTALLED)"
	rm -f "$ATTEMPTS"
	write_rc_local
	redirect_cloud
	restart_agent
	exit 0
fi

[ -n "$TAG" ] || { log "no tag and no binary installed; retry next boot"; exit 0; }

# Honour a previous give-up only for the SAME target tag. netinstall used to write a
# bare .gaveup marker and then skip on EVERY later boot — so a handful of transient
# failures (boot network not warm yet, clock skew, a flaky download) dead-ended the
# speaker forever, with no SSH-free way to recover, and install.sh would hang waiting
# for a version that never arrived. Now the marker carries the tag we gave up on: if a
# newer release exists, we clear it and retry automatically; only a working older
# binary on the exact same failed tag is left alone (so we don't re-download every boot
# while boseurls is still stuck). Remove $GAVEUP by hand to force a same-tag retry.
if [ -f "$GAVEUP" ]; then
	gave=$(cat "$GAVEUP" 2>/dev/null || echo "")
	if [ "$gave" = "$TAG" ] && [ -x "$BIN" ]; then
		log "gave up on $TAG earlier; same target — keeping ${INSTALLED:-none} (rm $GAVEUP to force)"
		write_rc_local
		redirect_cloud
		restart_agent
		exit 0
	fi
	log "previous give-up was for '${gave:-?}', target now '$TAG' — clearing and retrying"
	rm -f "$GAVEUP" "$ATTEMPTS"
fi

n=$(cat "$ATTEMPTS" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" >"$ATTEMPTS"
log "installing $TAG (have ${INSTALLED:-none}); attempt $n/$MAX_ATTEMPTS"
[ "$n" -gt "$MAX_ATTEMPTS" ] && giveup "exceeded $MAX_ATTEMPTS attempts"

DL=${RETOUCH_RELEASE_BASE:-https://github.com/$REPO/releases/download/$TAG}

# Download binary + checksums and verify before swapping it in.
curl -fsSL -o "$BIN.new" "$DL/retouch-armv7l" || { log "download failed"; exit 0; }
curl -fsSL -o "$HOME_DIR/SHA256SUMS" "$DL/SHA256SUMS" || { log "sums download failed"; exit 0; }
want=$(sed -n 's/ .*retouch-armv7l$//p' "$HOME_DIR/SHA256SUMS" | head -1)
got=$(openssl dgst -sha256 "$BIN.new" | sed 's/.*= //')
[ -n "$want" ] || giveup "no checksum in SHA256SUMS"
[ "$want" = "$got" ] || giveup "checksum mismatch ($got != $want)"
chmod 0755 "$BIN.new" && mv "$BIN.new" "$BIN"
echo "$TAG" > "$VERSION"
rm -f "$ATTEMPTS"
log "installed $TAG"

write_rc_local
redirect_cloud
restart_agent
# The boseurls/runtime cloud-URL cleanup + the reboot that makes it live are driven from
# install.sh once ReTouch is back online (the :17000 CLI is reliable there, not this
# early in boot).
log "done ($TAG); web UI on $WEB_LISTEN, marge on $MARGE_LISTEN. install.sh will finalise the cloud URLs."
