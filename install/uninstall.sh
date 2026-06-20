#!/bin/sh
# ReTouch on-speaker uninstaller / rollback. Runs ON the speaker as root. Restores the
# factory cloud config and removes the agent, its autostart, and the :8080 exposure.
# Safe to run repeatedly. Designed to work whether it is run by hand (over SSH) or
# launched wirelessly at boot via the same boseurls bootstrap the installer uses.
#
#   - removes the running agent, its autostart (NAND rc.local) and install dir
#   - removes the :8080 redirect and the :8000-hide rule
#   - restores SoundTouchSdkPrivateCfg.xml from the .original backup the installer made
#   - resets boseurls to a static, harmless value so a wireless (boot-bootstrap) run does
#     NOT re-trigger itself on every reboot
#   - reboots the speaker so the restored config takes effect (best-effort)
set -u

HOME_DIR=/mnt/nv/retouch
CFG=/opt/Bose/etc/SoundTouchSdkPrivateCfg.xml
LOG=/tmp/retouch-uninstall.log
APP_PORT=8000
# A static, non-executing placeholder. The installer points boseurls at a one-time
# "curl … | sh" bootstrap; if we ever run from that bootstrap we must overwrite it with
# something inert, or the speaker would re-run this uninstaller on every reboot.
DEAD_URL=http://retouch.invalid
log() { echo "[retouch-uninstall] $*" >>"$LOG" 2>&1; echo "[retouch-uninstall] $*"; }

# 1. stop the running agent.
pidof retouch >/dev/null 2>&1 && { kill "$(pidof retouch)" 2>/dev/null; log "stopped agent"; }

# 2. remove the autostart and the install dir so nothing relaunches on boot.
[ -f /mnt/nv/rc.local ] && { rm -f /mnt/nv/rc.local && log "removed /mnt/nv/rc.local"; }
[ -d "$HOME_DIR" ] && { rm -rf "$HOME_DIR" && log "removed $HOME_DIR"; }

# 3. drop the :8080 redirect and the :8000-hide rule the boot launcher installs. They are
# volatile (also clear on reboot) but drop them now so :8080 returns to Bose's setup server
# and :8000 is reachable again even without a reboot. Loops clear any duplicates.
if command -v iptables >/dev/null 2>&1; then
	while iptables -t nat -D PREROUTING -p tcp --dport 8080 -j REDIRECT --to-ports "$APP_PORT" 2>/dev/null; do :; done
	while iptables -t raw -D PREROUTING ! -i lo -p tcp --dport "$APP_PORT" -j DROP 2>/dev/null; do :; done
	# also clear the older :80 redirect from earlier versions, if present
	while iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$APP_PORT" 2>/dev/null; do :; done
	log "removed :8080 redirect + :8000-hide (if present)"
fi

# 4. restore the factory cloud config so the speaker stops pointing at the local stub.
if [ -f "$CFG.original" ]; then
	mount / -o rw,remount 2>>"$LOG" || mount -o remount,rw / 2>>"$LOG"
	cp "$CFG.original" "$CFG" && log "restored $CFG from .original"
	mount / -o ro,remount 2>/dev/null
else
	log "no $CFG.original backup found — config left as-is"
fi

# 5. break the boot bootstrap: overwrite boseurls with a static value so this uninstaller
# (when launched via the installer's boseurls "curl … | sh" hook) cannot re-run itself on
# every reboot. Best-effort — envswitch may not be on PATH when run by hand.
if command -v envswitch >/dev/null 2>&1; then
	envswitch boseurls set "$DEAD_URL" "$DEAD_URL/update" >>"$LOG" 2>&1 && log "reset boseurls to a static value"
else
	log "envswitch not found — boseurls left as-is (harmless unless it holds a bootstrap)"
fi

# 6. reboot so the restored config takes effect. Best-effort across the toolchains these
# speakers ship; if none work, fall back to asking the user.
log "rollback complete — rebooting"
sync 2>/dev/null
( sleep 2; /sbin/reboot 2>/dev/null || reboot 2>/dev/null || busybox reboot 2>/dev/null \
	|| log "could not reboot automatically — power-cycle the speaker or run ':17000 sys reboot'" ) &
