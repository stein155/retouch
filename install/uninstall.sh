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
SETUP_PORT=17000
MARGE_PORT=9080
log() { echo "[retouch-uninstall] $*" >>"$LOG" 2>&1; echo "[retouch-uninstall] $*"; }

# pidof can return several space-separated PIDs; pass them unquoted so kill gets
# each as its own argument (quoting makes "123 456" one bogus argument).
pids=$(pidof retouch 2>/dev/null) && [ -n "$pids" ] && { kill $pids 2>/dev/null; log "stopped agent"; }

# Remove the :8080 redirect and the :8000-hide rule the boot launcher installs. They are
# volatile (also clear on reboot) but drop them now so :8080 returns to Bose's setup
# server and :8000 is reachable again even without a reboot. Loops clear any duplicates.
if command -v iptables >/dev/null 2>&1; then
	while iptables -t nat -D PREROUTING -p tcp --dport 8080 -j REDIRECT --to-ports 8000 2>/dev/null; do :; done
	while iptables -t raw -D PREROUTING ! -i lo -p tcp --dport 8000 -j DROP 2>/dev/null; do :; done
	while iptables -t raw -D PREROUTING ! -i lo -p tcp --dport 17000 -j DROP 2>/dev/null; do :; done
	# also clear the older :80 redirect from earlier versions, if present
	while iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports 8000 2>/dev/null; do :; done
	# and the local :80 audio-notification auth redirect the boot launcher installs
	while iptables -t nat -D OUTPUT -p tcp -d 127.0.0.1 --dport 80 -j REDIRECT --to-ports "$MARGE_PORT" 2>/dev/null; do :; done
	log "removed :8080 redirect + :8000/:17000-hide + auth redirect (if present)"
fi

# Restore the config XML and, if we appended any audio-notification auth hosts to
# /etc/hosts, strip them again (both live on the ro rootfs, so remount once).
if [ -f "$CFG.original" ] || grep -q "127.0.0.1[[:space:]].*audionotification.api.bosecm.com" /etc/hosts 2>/dev/null; then
	mount / -o rw,remount 2>>"$LOG" || mount -o remount,rw / 2>>"$LOG"
	if [ -f "$CFG.original" ]; then
		cp "$CFG.original" "$CFG" && log "restored $CFG from .original"
	else
		log "no $CFG.original backup found — config left as-is"
	fi
	if [ -f /etc/hosts ] && grep -q "audionotification.api.bosecm.com" /etc/hosts 2>/dev/null; then
		grep -v "127.0.0.1[[:space:]].*audionotification.api.bosecm.com" /etc/hosts > /etc/hosts.rt.$$ 2>/dev/null \
			&& mv /etc/hosts.rt.$$ /etc/hosts && log "removed audio-notification auth hosts entries"
	fi
	mount / -o ro,remount 2>/dev/null
else
	log "no $CFG.original backup found — config left as-is"
fi

# The speaker's persisted cloud URLs (boseurls) still point at us — and may still
# hold the one-time bootstrap string if an install was interrupted. Clear them so a
# reboot can't re-run the installer, and blank the sys-configuration URLs the
# firmware reads. They only take effect after the reboot below.
if command -v nc >/dev/null 2>&1; then
	send() { printf '%s\n' "$1" | nc -w 3 127.0.0.1 "$SETUP_PORT" >/dev/null 2>&1; }
	# boseurls is the one-time bootstrap injection store; factory leaves it empty.
	send "envswitch boseurls set \"\" \"\""
	# Restore the sys-configuration cloud URLs to the FACTORY values captured in the
	# XML backup rather than blanking them: blank is not the factory state, and the
	# firmware may not fall back to the restored XML for these runtime keys.
	cfgurl() { [ -f "$CFG.original" ] && sed -n "s:.*<$1>\([^<]*\)</$1>.*:\1:p" "$CFG.original" | head -1; }
	for k in bmxRegistryUrl statsServerUrl margeServerUrl swUpdateUrl; do
		send "sys configuration $k \"$(cfgurl "$k")\""
	done
	log "restored sys-configuration cloud URLs from $CFG.original (boseurls cleared)"
fi

# Restore a pre-existing rc.local if we backed one up; otherwise remove ours.
if [ -f /mnt/nv/rc.local.original ]; then
	mv /mnt/nv/rc.local.original /mnt/nv/rc.local && log "restored original /mnt/nv/rc.local"
elif [ -f /mnt/nv/rc.local ]; then
	rm -f /mnt/nv/rc.local && log "removed /mnt/nv/rc.local"
fi
[ -d "$HOME_DIR" ] && { rm -rf "$HOME_DIR" && log "removed $HOME_DIR"; }

log "done. Reboot the speaker (':17000 sys reboot' or power-cycle) to read the restored config."
