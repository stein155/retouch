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
# The installer follows your terminal language when it can. Override it with one
# of the app languages if needed:
#
#   RETOUCH_LANG=nl curl -fsSL .../install.sh | sh
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
APP_URL_PORT=8080               # where ReTouch is exposed after install

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
die()  { printf '\n%s%s:%s %s\n' "$RED" "$(msg could_not_continue)" "$R" "$*" >&2; exit 1; }

# Read a line from the real keyboard even when this script is piped into `sh`.
ask() {
	if [ -r /dev/tty ]; then read -r REPLY < /dev/tty
	else REPLY=""; return 1; fi
}

# Match the app's supported languages. RETOUCH_LANG can override the terminal
# locale; unknown languages fall back to English.
detect_lang() {
	raw=${RETOUCH_LANG:-${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}}
	raw=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')
	raw=${raw%%.*}; raw=${raw%%_*}; raw=${raw%%-*}
	case "$raw" in en|de|nl|fr|es|af) printf '%s' "$raw" ;; *) printf 'en' ;; esac
}

LANG_CODE=$(detect_lang)

msg() {
	case "$LANG_CODE:$1" in
		en:could_not_continue) printf 'Could not continue' ;;
		en:need_curl) printf "this needs 'curl'. Please install it and try again." ;;
		en:need_nc) printf "this needs 'nc' (netcat). Install it with:\n       macOS:         it is already there - check your PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		en:title) printf 'ReTouch - internet radio for your Bose SoundTouch' ;;
		en:looking) printf 'Looking for Bose speakers on your network' ;;
		en:no_network) printf "could not work out your network automatically - you can type the address below." ;;
		en:found_speakers) printf 'Found %s Bose speaker(s):' ;;
		en:enter_address) printf 'Enter an address myself' ;;
		en:which_one) printf 'Which one?' ;;
		en:no_keyboard) printf 'no keyboard input available. Re-run and pass the address, e.g.  sh -s -- 192.168.1.42' ;;
		en:speaker_address) printf 'Speaker address (like 192.168.1.42): ' ;;
		en:no_speakers) printf "No speakers turned up on the scan - that is OK, you can type the address." ;;
		en:where_to_find) printf "(You will find it in the Bose app, or your router's device list.)" ;;
		en:no_address) printf 'no speaker address given.' ;;
		en:your_speaker) printf 'your speaker' ;;
		en:setting_up) printf 'Setting up ReTouch on %s (%s)' ;;
		en:restart_once) printf "This restarts the speaker once - it will be back in a minute or two." ;;
		en:sent_setup) printf 'sent the setup instructions' ;;
		en:couldnt_reach) printf "could not reach %s at %s. Check it is switched on and on the same network, then try again." ;;
		en:asked_restart) printf 'asked the speaker to restart' ;;
		en:waiting_restart) printf 'Waiting for the speaker to restart ' ;;
		en:waiting_restart_hint) printf '(first it goes offline)' ;;
		en:not_offline) printf 'the speaker did not go offline after the restart request.' ;;
		en:still_answering) printf 'ReTouch is still answering at %s, so the restart may not have started yet.' ;;
		en:retry_update) printf "Give it another minute, then run this installer again if it still has not updated." ;;
		en:waiting_online) printf 'Waiting for ReTouch to come online ' ;;
		en:waiting_online_hint) printf '(this takes a minute or two)' ;;
		en:ready) printf 'ReTouch is ready!' ;;
		en:open_here) printf 'Open it here:' ;;
		en:tip_open) printf 'Tip: open that link on your phone, then use Add to Home Screen.' ;;
		en:tip_app) printf 'It will then open and behave just like a normal app.' ;;
		en:not_answered) printf "ReTouch has not answered yet." ;;
		en:still_finishing_1) printf 'The speaker may still be finishing its restart. Give it another minute,' ;;
		en:still_finishing_2) printf 'then open %s in your browser.' ;;
		en:if_never) printf 'If it never comes up, just run this installer again.' ;;
		en:*) printf '%s' "$1" ;;

		nl:could_not_continue) printf 'Kan niet doorgaan' ;;
		nl:need_curl) printf "hiervoor is 'curl' nodig. Installeer het en probeer opnieuw." ;;
		nl:need_nc) printf "hiervoor is 'nc' (netcat) nodig. Installeer het met:\n       macOS:         dit staat er al op - controleer je PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		nl:title) printf 'ReTouch - internetradio voor je Bose SoundTouch' ;;
		nl:looking) printf 'Bose-speakers zoeken op je netwerk' ;;
		nl:no_network) printf 'kon je netwerk niet automatisch bepalen - je kunt het adres hieronder typen.' ;;
		nl:found_speakers) printf '%s Bose-speaker(s) gevonden:' ;;
		nl:enter_address) printf 'Zelf een adres invoeren' ;;
		nl:which_one) printf 'Welke?' ;;
		nl:no_keyboard) printf 'geen toetsenbordinvoer beschikbaar. Start opnieuw en geef het adres mee, bijv.  sh -s -- 192.168.1.42' ;;
		nl:speaker_address) printf 'Speakeradres (zoals 192.168.1.42): ' ;;
		nl:no_speakers) printf 'Er zijn geen speakers gevonden - geen probleem, je kunt het adres typen.' ;;
		nl:where_to_find) printf '(Je vindt het in de Bose-app, of in de apparatenlijst van je router.)' ;;
		nl:no_address) printf 'geen speakeradres opgegeven.' ;;
		nl:your_speaker) printf 'je speaker' ;;
		nl:setting_up) printf 'ReTouch instellen op %s (%s)' ;;
		nl:restart_once) printf 'De speaker wordt een keer herstart - hij is over een minuut of twee terug.' ;;
		nl:sent_setup) printf 'installatie-instructies verzonden' ;;
		nl:couldnt_reach) printf 'kon %s niet bereiken op %s. Controleer of hij aan staat en op hetzelfde netwerk zit, en probeer opnieuw.' ;;
		nl:asked_restart) printf 'speaker gevraagd om te herstarten' ;;
		nl:waiting_restart) printf 'Wachten tot de speaker herstart ' ;;
		nl:waiting_restart_hint) printf '(eerst gaat hij offline)' ;;
		nl:not_offline) printf 'de speaker ging niet offline na het herstartverzoek.' ;;
		nl:still_answering) printf 'ReTouch antwoordt nog steeds op %s, dus de herstart is misschien nog niet begonnen.' ;;
		nl:retry_update) printf 'Wacht nog een minuut en start deze installer opnieuw als hij nog niet is bijgewerkt.' ;;
		nl:waiting_online) printf 'Wachten tot ReTouch online komt ' ;;
		nl:waiting_online_hint) printf '(dit duurt een minuut of twee)' ;;
		nl:ready) printf 'ReTouch is klaar!' ;;
		nl:open_here) printf 'Open hem hier:' ;;
		nl:tip_open) printf 'Tip: open die link op je telefoon en gebruik daarna Zet op beginscherm.' ;;
		nl:tip_app) printf 'Hij opent daarna als een normale app.' ;;
		nl:not_answered) printf 'ReTouch heeft nog niet geantwoord.' ;;
		nl:still_finishing_1) printf 'De speaker is misschien nog bezig met herstarten. Wacht nog een minuut,' ;;
		nl:still_finishing_2) printf 'en open daarna %s in je browser.' ;;
		nl:if_never) printf 'Als hij niet opkomt, start deze installer dan opnieuw.' ;;

		de:could_not_continue) printf 'Kann nicht fortfahren' ;;
		de:need_curl) printf "dafuer wird 'curl' benoetigt. Bitte installiere es und versuche es erneut." ;;
		de:need_nc) printf "dafuer wird 'nc' (netcat) benoetigt. Installiere es mit:\n       macOS:         ist bereits vorhanden - pruefe deinen PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		de:title) printf 'ReTouch - Internetradio fuer deinen Bose SoundTouch' ;;
		de:looking) printf 'Suche Bose-Lautsprecher in deinem Netzwerk' ;;
		de:no_network) printf 'konnte dein Netzwerk nicht automatisch erkennen - du kannst die Adresse unten eingeben.' ;;
		de:found_speakers) printf '%s Bose-Lautsprecher gefunden:' ;;
		de:enter_address) printf 'Adresse selbst eingeben' ;;
		de:which_one) printf 'Welcher?' ;;
		de:no_keyboard) printf 'keine Tastatureingabe verfuegbar. Starte erneut und uebergib die Adresse, z. B.  sh -s -- 192.168.1.42' ;;
		de:speaker_address) printf 'Lautsprecheradresse (wie 192.168.1.42): ' ;;
		de:no_speakers) printf 'Beim Scan wurden keine Lautsprecher gefunden - du kannst die Adresse eingeben.' ;;
		de:where_to_find) printf '(Du findest sie in der Bose-App oder in der Geraeteliste deines Routers.)' ;;
		de:no_address) printf 'keine Lautsprecheradresse angegeben.' ;;
		de:your_speaker) printf 'dein Lautsprecher' ;;
		de:setting_up) printf 'ReTouch wird auf %s (%s) eingerichtet' ;;
		de:restart_once) printf 'Der Lautsprecher startet einmal neu - er ist in ein bis zwei Minuten wieder da.' ;;
		de:sent_setup) printf 'Installationsanweisungen gesendet' ;;
		de:couldnt_reach) printf 'konnte %s unter %s nicht erreichen. Pruefe, ob er eingeschaltet und im selben Netzwerk ist, und versuche es erneut.' ;;
		de:asked_restart) printf 'Lautsprecher zum Neustart aufgefordert' ;;
		de:waiting_restart) printf 'Warte auf den Neustart des Lautsprechers ' ;;
		de:waiting_restart_hint) printf '(zuerst geht er offline)' ;;
		de:not_offline) printf 'der Lautsprecher ging nach der Neustartanforderung nicht offline.' ;;
		de:still_answering) printf 'ReTouch antwortet noch unter %s, der Neustart hat also moeglicherweise noch nicht begonnen.' ;;
		de:retry_update) printf 'Warte noch eine Minute und starte diesen Installer erneut, falls er noch nicht aktualisiert wurde.' ;;
		de:waiting_online) printf 'Warte, bis ReTouch online ist ' ;;
		de:waiting_online_hint) printf '(das dauert ein bis zwei Minuten)' ;;
		de:ready) printf 'ReTouch ist bereit!' ;;
		de:open_here) printf 'Hier oeffnen:' ;;
		de:tip_open) printf 'Tipp: Oeffne den Link auf deinem Telefon und nutze Zum Home-Bildschirm.' ;;
		de:tip_app) printf 'Danach oeffnet es sich wie eine normale App.' ;;
		de:not_answered) printf 'ReTouch hat noch nicht geantwortet.' ;;
		de:still_finishing_1) printf 'Der Lautsprecher beendet moeglicherweise noch seinen Neustart. Warte noch eine Minute,' ;;
		de:still_finishing_2) printf 'und oeffne dann %s in deinem Browser.' ;;
		de:if_never) printf 'Wenn es nicht startet, fuehre diesen Installer einfach erneut aus.' ;;

		fr:could_not_continue) printf 'Impossible de continuer' ;;
		fr:need_curl) printf "'curl' est necessaire. Installez-le puis reessayez." ;;
		fr:need_nc) printf "'nc' (netcat) est necessaire. Installez-le avec :\n       macOS:         il est deja present - verifiez votre PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		fr:title) printf 'ReTouch - radio Internet pour votre Bose SoundTouch' ;;
		fr:looking) printf 'Recherche des enceintes Bose sur votre reseau' ;;
		fr:no_network) printf "impossible de detecter votre reseau automatiquement - vous pouvez saisir l'adresse ci-dessous." ;;
		fr:found_speakers) printf '%s enceinte(s) Bose trouvee(s) :' ;;
		fr:enter_address) printf 'Saisir une adresse moi-meme' ;;
		fr:which_one) printf 'Laquelle ?' ;;
		fr:no_keyboard) printf "aucune saisie clavier disponible. Relancez avec l'adresse, par ex.  sh -s -- 192.168.1.42" ;;
		fr:speaker_address) printf "Adresse de l'enceinte (comme 192.168.1.42) : " ;;
		fr:no_speakers) printf "Aucune enceinte trouvee pendant le scan - ce n'est pas grave, vous pouvez saisir l'adresse." ;;
		fr:where_to_find) printf "(Vous la trouverez dans l'app Bose ou dans la liste des appareils de votre routeur.)" ;;
		fr:no_address) printf "aucune adresse d'enceinte indiquee." ;;
		fr:your_speaker) printf 'votre enceinte' ;;
		fr:setting_up) printf 'Configuration de ReTouch sur %s (%s)' ;;
		fr:restart_once) printf "L'enceinte redemarre une fois - elle sera de retour dans une minute ou deux." ;;
		fr:sent_setup) printf 'instructions de configuration envoyees' ;;
		fr:couldnt_reach) printf "impossible de joindre %s a %s. Verifiez qu'elle est allumee et sur le meme reseau, puis reessayez." ;;
		fr:asked_restart) printf "redemarrage de l'enceinte demande" ;;
		fr:waiting_restart) printf "Attente du redemarrage de l'enceinte " ;;
		fr:waiting_restart_hint) printf "(elle passe d'abord hors ligne)" ;;
		fr:not_offline) printf "l'enceinte n'est pas passee hors ligne apres la demande de redemarrage." ;;
		fr:still_answering) printf "ReTouch repond encore a %s, le redemarrage n'a donc peut-etre pas encore commence." ;;
		fr:retry_update) printf "Attendez encore une minute, puis relancez cet installateur si la mise a jour n'est toujours pas faite." ;;
		fr:waiting_online) printf 'Attente de ReTouch en ligne ' ;;
		fr:waiting_online_hint) printf '(cela prend une minute ou deux)' ;;
		fr:ready) printf 'ReTouch est pret !' ;;
		fr:open_here) printf 'Ouvrez-le ici :' ;;
		fr:tip_open) printf "Astuce : ouvrez ce lien sur votre telephone, puis utilisez Ajouter a l'ecran d'accueil." ;;
		fr:tip_app) printf "Il s'ouvrira ensuite comme une app normale." ;;
		fr:not_answered) printf "ReTouch n'a pas encore repondu." ;;
		fr:still_finishing_1) printf "L'enceinte termine peut-etre encore son redemarrage. Attendez une minute," ;;
		fr:still_finishing_2) printf 'puis ouvrez %s dans votre navigateur.' ;;
		fr:if_never) printf "S'il ne demarre jamais, relancez simplement cet installateur." ;;

		es:could_not_continue) printf 'No se pudo continuar' ;;
		es:need_curl) printf "se necesita 'curl'. Instalalo e intentalo de nuevo." ;;
		es:need_nc) printf "se necesita 'nc' (netcat). Instalalo con:\n       macOS:         ya esta incluido - revisa tu PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		es:title) printf 'ReTouch - radio por Internet para tu Bose SoundTouch' ;;
		es:looking) printf 'Buscando altavoces Bose en tu red' ;;
		es:no_network) printf 'no se pudo detectar tu red automaticamente - puedes escribir la direccion abajo.' ;;
		es:found_speakers) printf 'Se encontraron %s altavoz/altavoces Bose:' ;;
		es:enter_address) printf 'Introducir una direccion manualmente' ;;
		es:which_one) printf 'Cual?' ;;
		es:no_keyboard) printf 'no hay entrada de teclado disponible. Ejecuta de nuevo y pasa la direccion, p. ej.  sh -s -- 192.168.1.42' ;;
		es:speaker_address) printf 'Direccion del altavoz (como 192.168.1.42): ' ;;
		es:no_speakers) printf 'No aparecieron altavoces en el escaneo - no pasa nada, puedes escribir la direccion.' ;;
		es:where_to_find) printf '(La encontraras en la app de Bose o en la lista de dispositivos de tu router.)' ;;
		es:no_address) printf 'no se indico ninguna direccion de altavoz.' ;;
		es:your_speaker) printf 'tu altavoz' ;;
		es:setting_up) printf 'Configurando ReTouch en %s (%s)' ;;
		es:restart_once) printf 'El altavoz se reinicia una vez - volvera en uno o dos minutos.' ;;
		es:sent_setup) printf 'instrucciones de configuracion enviadas' ;;
		es:couldnt_reach) printf 'no se pudo contactar con %s en %s. Comprueba que este encendido y en la misma red, e intentalo de nuevo.' ;;
		es:asked_restart) printf 'se pidio al altavoz que se reiniciara' ;;
		es:waiting_restart) printf 'Esperando a que el altavoz se reinicie ' ;;
		es:waiting_restart_hint) printf '(primero se desconecta)' ;;
		es:not_offline) printf 'el altavoz no se desconecto despues de pedir el reinicio.' ;;
		es:still_answering) printf 'ReTouch sigue respondiendo en %s, asi que puede que el reinicio aun no haya empezado.' ;;
		es:retry_update) printf 'Espera otro minuto y ejecuta este instalador de nuevo si aun no se ha actualizado.' ;;
		es:waiting_online) printf 'Esperando a que ReTouch este en linea ' ;;
		es:waiting_online_hint) printf '(esto tarda uno o dos minutos)' ;;
		es:ready) printf 'ReTouch esta listo!' ;;
		es:open_here) printf 'Abrelo aqui:' ;;
		es:tip_open) printf 'Consejo: abre ese enlace en tu telefono y usa Anadir a pantalla de inicio.' ;;
		es:tip_app) printf 'Luego se abrira y se comportara como una app normal.' ;;
		es:not_answered) printf 'ReTouch aun no ha respondido.' ;;
		es:still_finishing_1) printf 'Puede que el altavoz aun este terminando de reiniciarse. Espera otro minuto,' ;;
		es:still_finishing_2) printf 'y abre %s en tu navegador.' ;;
		es:if_never) printf 'Si nunca aparece, ejecuta este instalador de nuevo.' ;;

		af:could_not_continue) printf 'Kan nie voortgaan nie' ;;
		af:need_curl) printf "hierdie het 'curl' nodig. Installeer dit en probeer weer." ;;
		af:need_nc) printf "hierdie het 'nc' (netcat) nodig. Installeer dit met:\n       macOS:         dit is reeds daar - kyk jou PATH\n       Debian/Ubuntu: sudo apt install netcat-openbsd\n       Fedora:        sudo dnf install nmap-ncat" ;;
		af:title) printf 'ReTouch - internetradio vir jou Bose SoundTouch' ;;
		af:looking) printf 'Soek vir Bose-luidsprekers op jou netwerk' ;;
		af:no_network) printf 'kon nie jou netwerk outomaties bepaal nie - jy kan die adres hieronder tik.' ;;
		af:found_speakers) printf '%s Bose-luidspreker(s) gevind:' ;;
		af:enter_address) printf 'Voer self n adres in' ;;
		af:which_one) printf 'Watter een?' ;;
		af:no_keyboard) printf 'geen sleutelbordinvoer beskikbaar nie. Begin weer en gee die adres, bv.  sh -s -- 192.168.1.42' ;;
		af:speaker_address) printf 'Luidsprekeradres (soos 192.168.1.42): ' ;;
		af:no_speakers) printf 'Geen luidsprekers is met die skandering gevind nie - dis reg, jy kan die adres tik.' ;;
		af:where_to_find) printf '(Jy kry dit in die Bose-app, of in jou router se toestellys.)' ;;
		af:no_address) printf 'geen luidsprekeradres gegee nie.' ;;
		af:your_speaker) printf 'jou luidspreker' ;;
		af:setting_up) printf 'Stel ReTouch op %s (%s) op' ;;
		af:restart_once) printf 'Die luidspreker herbegin een keer - dit is oor n minuut of twee terug.' ;;
		af:sent_setup) printf 'opstelinstruksies gestuur' ;;
		af:couldnt_reach) printf 'kon %s by %s nie bereik nie. Kyk dat dit aangeskakel is en op dieselfde netwerk is, en probeer weer.' ;;
		af:asked_restart) printf 'luidspreker gevra om te herbegin' ;;
		af:waiting_restart) printf 'Wag dat die luidspreker herbegin ' ;;
		af:waiting_restart_hint) printf '(eers gaan dit vanlyn)' ;;
		af:not_offline) printf 'die luidspreker het nie vanlyn gegaan na die herbeginversoek nie.' ;;
		af:still_answering) printf 'ReTouch antwoord nog by %s, so die herbegin het dalk nog nie begin nie.' ;;
		af:retry_update) printf 'Wag nog n minuut en voer hierdie installeerder weer uit as dit nog nie opgedateer het nie.' ;;
		af:waiting_online) printf 'Wag dat ReTouch aanlyn kom ' ;;
		af:waiting_online_hint) printf '(dit neem n minuut of twee)' ;;
		af:ready) printf 'ReTouch is gereed!' ;;
		af:open_here) printf 'Maak dit hier oop:' ;;
		af:tip_open) printf 'Wenk: maak daardie skakel op jou foon oop en gebruik Voeg by tuisskerm.' ;;
		af:tip_app) printf 'Dit sal dan oopmaak en soos n gewone app werk.' ;;
		af:not_answered) printf 'ReTouch het nog nie geantwoord nie.' ;;
		af:still_finishing_1) printf 'Die luidspreker is dalk nog besig om te herbegin. Wag nog n minuut,' ;;
		af:still_finishing_2) printf 'en maak dan %s in jou blaaier oop.' ;;
		af:if_never) printf 'As dit nooit opkom nie, voer hierdie installeerder net weer uit.' ;;

		*) old=$LANG_CODE; LANG_CODE=en; msg "$1"; LANG_CODE=$old ;;
	esac
}

fmt() { template=$(msg "$1"); shift; printf "$template" "$@"; }

# ---- preflight -------------------------------------------------------------
command -v curl >/dev/null 2>&1 || die "$(msg need_curl)"
if ! command -v nc >/dev/null 2>&1; then
	die "$(msg need_nc)"
fi

say ""
say "${B}$(msg title)${R}"
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
		printf '%s%s' "$(msg looking)" "$DIM"
		scan "$SUB" &
		sp=$!
		while kill -0 "$sp" 2>/dev/null; do printf '.'; sleep 1; done
		wait "$sp" 2>/dev/null
		printf '%s\n\n' "$R"
	else
		warn "$(msg no_network)"
	fi

	count=$(wc -l < "$FOUND" 2>/dev/null | tr -d ' '); count=${count:-0}
	if [ "$count" -gt 0 ]; then
		say "$(fmt found_speakers "${B}$count${R}")"
		say ""
		i=1
		while IFS="$(printf '\t')" read -r sip sname stype; do
			printf '  %s%2d)%s  %-22s %s%s%s  %s\n' "$B" "$i" "$R" "$sname" "$DIM" "$stype" "$R" "$sip"
			i=$((i + 1))
		done < "$FOUND"
		printf '  %s%2d)%s  %s\n' "$B" "$i" "$R" "$(msg enter_address)"
		say ""
		printf '%s %s[1]%s ' "$(msg which_one)" "$DIM" "$R"
		ask || die "$(msg no_keyboard)"
		choice=${REPLY:-1}
		if [ "$choice" = "$i" ]; then
			printf '%s' "$(msg speaker_address)"
			ask; IP="$REPLY"
		else
			IP=$(sed -n "${choice}p" "$FOUND" | cut -f1)
		fi
	else
		say "$(msg no_speakers)"
		say "${DIM}$(msg where_to_find)${R}"
		say ""
		printf '%s' "$(msg speaker_address)"
		ask || die "$(msg no_keyboard)"
		IP="$REPLY"
	fi
fi

IP=$(printf '%s' "$IP" | tr -d ' ')
[ -n "$IP" ] || die "$(msg no_address)"

# Friendly name for the chosen speaker, if we know it.
NAME=$(grep -E "^$IP	" "$FOUND" 2>/dev/null | cut -f2)
[ -n "$NAME" ] || NAME=$(curl -fsS --connect-timeout 1 --max-time 2 "http://$IP:$API_PORT/info" 2>/dev/null \
	| tr -d '\r\n' | sed -n 's:.*<name>\([^<]*\)</name>.*:\1:p')
[ -n "$NAME" ] || NAME="$(msg your_speaker)"

# ---- set it up -------------------------------------------------------------
say ""
say "$(fmt setting_up "${B}$NAME${R}" "${DIM}$IP${R}")"
say "$(msg restart_once)"
say ""

send() { printf '%s\n' "$1" | nc -w 3 "$IP" "$SETUP_PORT" >/dev/null 2>&1; }
URL="http://$IP:$APP_URL_PORT"
was_up=0
curl -fsS --connect-timeout 1 --max-time 2 "$URL/api/settings" >/dev/null 2>&1 && was_up=1

# Hand the speaker a one-time instruction to fetch and run the on-speaker setup,
# then tell it to restart so the instruction takes effect.
if send "envswitch boseurls set \"$PLACE;curl -sSL $NETINSTALL -o /tmp/b;sh /tmp/b\" \"$PLACE/update\""; then
	ok "$(msg sent_setup)"
else
	die "$(fmt couldnt_reach "$NAME" "$IP")"
fi
send "sys reboot"
ok "$(msg asked_restart)"

# ---- wait for ReTouch to come up ------------------------------------------
say ""
# When updating an already running install, the old ReTouch can still answer for a
# short while after the reboot request. Wait for that instance to disappear first,
# otherwise we can report success before the speaker has actually restarted.
if [ "$was_up" -eq 1 ]; then
	printf '%s%s%s%s' "$(msg waiting_restart)" "$DIM" "$(msg waiting_restart_hint)" "$R"
	down=0
	n=0
	while [ "$n" -lt 60 ]; do            # ~2 minutes for the old service to stop
		if ! curl -fsS --connect-timeout 1 --max-time 2 "$URL/api/settings" >/dev/null 2>&1; then
			down=1; break
		fi
		printf '.'
		sleep 2
		n=$((n + 1))
	done
	printf '\n\n'
	if [ "$down" -ne 1 ]; then
		warn "$(msg not_offline)"
		say ""
		say "  $(fmt still_answering "${B}$URL${R}")"
		say "  $(msg retry_update)"
		say ""
		exit 0
	fi
fi

printf '%s%s%s%s' "$(msg waiting_online)" "$DIM" "$(msg waiting_online_hint)" "$R"
# Probe the app's own API (/api/settings only answers from ReTouch, so Bose's setup
# page can't look like a false "ready"). ReTouch is exposed on exactly one uniform
# port — :8080 — on every speaker, so that is the only URL we wait for and advertise.
up=0
n=0
while [ "$n" -lt 90 ]; do            # ~6 minutes, plenty for a reboot + setup
	if curl -fsS --connect-timeout 2 --max-time 3 "$URL/api/settings" >/dev/null 2>&1; then
		up=1; break
	fi
	printf '.'
	sleep 4
	n=$((n + 1))
done
printf '\n\n'

if [ "$up" -eq 1 ]; then
	ok "${B}$(msg ready)${R}"
	say ""
	say "  $(msg open_here)"
	say ""
	say "      ${B}$URL${R}"
	say ""
	say "  ${DIM}$(msg tip_open)${R}"
	say "  ${DIM}$(msg tip_app)${R}"
	say ""
else
	warn "$(msg not_answered)"
	say ""
	say "  $(msg still_finishing_1)"
	say "  $(fmt still_finishing_2 "${B}$URL${R}")"
	say ""
	say "  ${DIM}$(msg if_never)${R}"
	say ""
fi
