#!/bin/sh
# ReTouch installer. Run it straight from the web — no download, no clone:
#
#   curl -fsSL https://raw.githubusercontent.com/stein155/retouch/main/install/install.sh | sh
#
# It finds Bose speakers on your network, lets you pick one, sets ReTouch up over
# the air, and tells you the link to open. Nothing is installed on your computer.
#
# You can also point it straight at a speaker if you already know its address:
#
#   curl -fsSL .../install.sh | sh -s -- 192.168.1.42
#
# Needs two common command-line tools: curl and nc (netcat). Both ship with macOS
# and most Linux systems.
set -u

REPO=stein155/retouch
BRANCH=main
NETINSTALL="https://raw.githubusercontent.com/$REPO/$BRANCH/install/netinstall.sh"
PLACE="http://x.invalid"        # harmless placeholder the speaker overwrites itself

API_PORT=8090                   # speakers answer here; used only to find them
APP_PORT=8000                   # ReTouch's web app; also reachable on :80 via redirect
SETUP_PORT=17000                # where we hand the speaker its setup instructions

# ---- pretty output ---------------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$(printf '\033[1m'); DIM=$(printf '\033[2m'); GRN=$(printf '\033[32m')
	YEL=$(printf '\033[33m'); RED=$(printf '\033[31m'); R=$(printf '\033[0m')
else
	B=; DIM=; GRN=; YEL=; RED=; R=
fi
say()  { printf '%s\n' "$*"; }
ok()   { printf '  %s✓%s %s\n' "$GRN" "$R" "$*"; }
warn() { printf '  %s!%s %s\n' "$YEL" "$R" "$*"; }
die()  { printf '\n%sCould not continue:%s %s\n' "$RED" "$R" "$*" >&2; exit 1; }

# Read a line from the real keyboard even when this script is piped into `sh`.
ask() {
	if [ -r /dev/tty ]; then read -r REPLY < /dev/tty
	else REPLY=""; return 1; fi
}

# ---- preflight -------------------------------------------------------------
command -v curl >/dev/null 2>&1 || die "this needs 'curl'. Please install it and try again."
if ! command -v nc >/dev/null 2>&1; then
	die "this needs 'nc' (netcat). Install it with:
       macOS:         it's already there — check your PATH
       Debian/Ubuntu: sudo apt install netcat-openbsd
       Fedora:        sudo dnf install nmap-ncat"
fi

say ""
say "${B}ReTouch${R} — internet radio for your Bose SoundTouch"
say "${DIM}─────────────────────────────────────────────${R}"
say ""

# ---- figure out our network ------------------------------------------------
# Best-effort across Linux and macOS; we only need the first three numbers of
# our own address (e.g. 192.168.1) to know which network to look on.
my_ip() {
	ip=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \([0-9.][0-9.]*\).*/\1/p' | head -1)
	[ -n "${ip:-}" ] || ip=$(ipconfig getifaddr en0 2>/dev/null)
	[ -n "${ip:-}" ] || ip=$(ipconfig getifaddr en1 2>/dev/null)
	[ -n "${ip:-}" ] || ip=$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9]' | head -1)
	printf '%s' "${ip:-}"
}

# Ask one address whether it's a Bose speaker; if so, record "ip<TAB>name<TAB>type".
probe() {
	xml=$(curl -fsS --connect-timeout 1 --max-time 2 "http://$1:$API_PORT/info" 2>/dev/null) || return 0
	case "$xml" in *deviceID*) ;; *) return 0 ;; esac
	name=$(printf '%s' "$xml" | tr -d '\r\n' | sed -n 's:.*<name>\([^<]*\)</name>.*:\1:p')
	type=$(printf '%s' "$xml" | tr -d '\r\n' | sed -n 's:.*<type>\([^<]*\)</type>.*:\1:p')
	printf '%s\t%s\t%s\n' "$1" "${name:-Bose speaker}" "${type:-SoundTouch}" >> "$FOUND"
}

# Scan the whole local network for speakers, in parallel batches.
scan() {
	base=$1
	: > "$FOUND"
	n=1
	while [ "$n" -le 254 ]; do
		probe "$base.$n" &
		[ $((n % 50)) -eq 0 ] && wait
		n=$((n + 1))
	done
	wait
	[ -s "$FOUND" ] && sort -t. -k4 -n "$FOUND" -o "$FOUND" 2>/dev/null
}

TMP=$(mktemp -d 2>/dev/null || echo "/tmp/retouch.$$")
mkdir -p "$TMP" 2>/dev/null
FOUND="$TMP/found"
: > "$FOUND"
trap 'rm -rf "$TMP" 2>/dev/null' EXIT

# ---- pick a speaker --------------------------------------------------------
IP="${1:-}"

if [ -z "$IP" ]; then
	SUB=$(my_ip | sed 's/\.[0-9]*$//')
	if [ -n "$SUB" ]; then
		printf 'Looking for Bose speakers on your network%s' "$DIM"
		scan "$SUB" &
		sp=$!
		while kill -0 "$sp" 2>/dev/null; do printf '.'; sleep 1; done
		wait "$sp" 2>/dev/null
		printf '%s\n\n' "$R"
	else
		warn "couldn't work out your network automatically — you can type the address below."
	fi

	count=$(wc -l < "$FOUND" 2>/dev/null | tr -d ' '); count=${count:-0}
	if [ "$count" -gt 0 ]; then
		say "Found ${B}$count${R} speaker$( [ "$count" -gt 1 ] && echo s ):"
		say ""
		i=1
		while IFS="$(printf '\t')" read -r sip sname stype; do
			printf '  %s%2d)%s  %-22s %s%s%s  %s\n' "$B" "$i" "$R" "$sname" "$DIM" "$stype" "$R" "$sip"
			i=$((i + 1))
		done < "$FOUND"
		printf '  %s%2d)%s  Enter an address myself\n' "$B" "$i" "$R"
		say ""
		printf 'Which one? %s[1]%s ' "$DIM" "$R"
		ask || die "no keyboard input available. Re-run and pass the address, e.g.  sh -s -- 192.168.1.42"
		choice=${REPLY:-1}
		if [ "$choice" = "$i" ]; then
			printf 'Speaker address (like 192.168.1.42): '
			ask; IP="$REPLY"
		else
			IP=$(sed -n "${choice}p" "$FOUND" | cut -f1)
		fi
	else
		say "No speakers turned up on the scan — that's OK, you can type the address."
		say "${DIM}(You'll find it in the Bose app, or your router's device list.)${R}"
		say ""
		printf 'Speaker address (like 192.168.1.42): '
		ask || die "no keyboard input available. Re-run and pass the address, e.g.  sh -s -- 192.168.1.42"
		IP="$REPLY"
	fi
fi

IP=$(printf '%s' "$IP" | tr -d ' ')
[ -n "$IP" ] || die "no speaker address given."

# Friendly name for the chosen speaker, if we know it.
NAME=$(grep -E "^$IP	" "$FOUND" 2>/dev/null | cut -f2)
[ -n "$NAME" ] || NAME=$(curl -fsS --connect-timeout 1 --max-time 2 "http://$IP:$API_PORT/info" 2>/dev/null \
	| tr -d '\r\n' | sed -n 's:.*<name>\([^<]*\)</name>.*:\1:p')
[ -n "$NAME" ] || NAME="your speaker"

# ---- set it up -------------------------------------------------------------
say ""
say "Setting up ReTouch on ${B}$NAME${R} ${DIM}($IP)${R}"
say "This restarts the speaker once — it'll be back in a minute or two."
say ""

send() { printf '%s\n' "$1" | nc -w 3 "$IP" "$SETUP_PORT" >/dev/null 2>&1; }

# Hand the speaker a one-time instruction to fetch and run the on-speaker setup,
# then tell it to restart so the instruction takes effect.
if send "envswitch boseurls set \"$PLACE;curl -sSL $NETINSTALL -o /tmp/b;sh /tmp/b\" \"$PLACE/update\""; then
	ok "sent the setup instructions"
else
	die "couldn't reach $NAME at $IP. Check it's switched on and on the same network, then try again."
fi
send "sys reboot"
ok "asked the speaker to restart"

# ---- wait for ReTouch to come up ------------------------------------------
say ""
printf 'Waiting for ReTouch to come online %s(this takes a minute or two)%s' "$DIM" "$R"
# Probe the app itself (its API), not bare :80 — Bose's own :80 setup page would
# otherwise look like a false "ready". /api/settings only answers from ReTouch.
PROBE="http://$IP:$APP_PORT/api/settings"
URL="http://$IP"                 # what the user opens; :80 is redirected to the app
up=0
n=0
while [ "$n" -lt 90 ]; do            # ~6 minutes, plenty for a reboot + setup
	if curl -fsS --connect-timeout 2 --max-time 3 "$PROBE" >/dev/null 2>&1; then up=1; break; fi
	printf '.'
	sleep 4
	n=$((n + 1))
done
printf '\n\n'

if [ "$up" -eq 1 ]; then
	# Prefer the clean :80 URL, but only advertise it if the redirect really lands
	# on ReTouch (some speakers may not allow the redirect — then use :8000).
	if ! curl -fsS --connect-timeout 2 --max-time 3 "http://$IP/api/settings" >/dev/null 2>&1; then
		URL="http://$IP:$APP_PORT"
	fi
	ok "${B}ReTouch is ready!${R}"
	say ""
	say "  Open it here:"
	say ""
	say "      ${B}$URL${R}"
	say ""
	say "  ${DIM}Tip:${R} open that link on your phone, then use ${B}Add to Home Screen${R}."
	say "  ${DIM}It'll then open and behave just like a normal app.${R}"
	say ""
else
	warn "ReTouch hasn't answered yet."
	say ""
	say "  The speaker may still be finishing its restart. Give it another minute,"
	say "  then open ${B}$URL${R} in your browser."
	say ""
	say "  ${DIM}If it never comes up, just run this installer again.${R}"
	say ""
fi
