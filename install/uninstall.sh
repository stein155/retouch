#!/bin/sh
# ReTouch on-speaker uninstaller / rollback. Runs ON the speaker as root. Restores the
# factory cloud URLs and removes the agent + autostart. Safe to run repeatedly.
#
#   - restores SoundTouchSdkPrivateCfg.xml from the .original backup the installer made
#   - removes the NAND rc.local autostart line
#   - stops the running agent and removes its install dir
#
# After running, reboot the speaker so it reads the restored config.
set -u

HOME_DIR=/mnt/nv/retouch
CFG=/opt/Bose/etc/SoundTouchSdkPrivateCfg.xml
LOG=/tmp/retouch-uninstall.log
log() { echo "[retouch-uninstall] $*" >>"$LOG" 2>&1; echo "[retouch-uninstall] $*"; }

pidof retouch >/dev/null 2>&1 && { kill "$(pidof retouch)" 2>/dev/null; log "stopped agent"; }

if [ -f "$CFG.original" ]; then
	mount / -o rw,remount 2>>"$LOG" || mount -o remount,rw / 2>>"$LOG"
	cp "$CFG.original" "$CFG" && log "restored $CFG from .original"
	mount / -o ro,remount 2>/dev/null
else
	log "no $CFG.original backup found — config left as-is"
fi

[ -f /mnt/nv/rc.local ] && { rm -f /mnt/nv/rc.local && log "removed /mnt/nv/rc.local"; }
[ -d "$HOME_DIR" ] && { rm -rf "$HOME_DIR" && log "removed $HOME_DIR"; }

log "done. Reboot the speaker (':17000 sys reboot' or power-cycle) to read the restored config."
