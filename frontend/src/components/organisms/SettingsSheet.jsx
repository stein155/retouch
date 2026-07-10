import { useState, useEffect, useRef, useCallback } from 'react';
import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { Spinner } from '../atoms/Spinner';
import { Skeleton } from '../atoms/Skeleton';
import { Button } from '../atoms/Button';
import { Toggle } from '../atoms/Toggle';
import { BassSlider } from '../molecules/BassSlider';
import { SpeakerRow } from '../molecules/SpeakerRow';
import { PluginsSection, PluginSettings } from './PluginsSection';
import {
  SheetScrim, SheetEl, SheetHandle, SheetBody, SheetHeader,
} from '../molecules/Sheet';
import {
  Form, FormSection, FieldHint, FieldCard, FieldRow, FieldRowLabel, FieldRowInput,
  FieldRowValue, Select, SelectWrap, SelectChev, SetEyebrow,
  BassCard, BassHead, BassName, BassVal, BassScale,
} from '../molecules/Field';
import { useI18n, LANGS } from '../../lib/i18n';
import {
  getSettings, saveSettings, getVersion, getReleases, startUpdate,
  findSpeakers, groupSpeaker, ungroupSpeaker,
  getMqttStatus, getAuth, login, logout, setPassword,
} from '../../lib/api';

const fmtBass = (v) => (v > 0 ? '+' + v : String(v));
const betaUpdatesKey = 'retouch-beta-updates';

// Map the speaker's signal token to a localised label.
const sigLabel = (t, sig) => {
  const key = { excellent: 'sigExcellent', good: 'sigGood', fair: 'sigFair', poor: 'sigPoor' }[sig];
  return key ? t(key) : sig;
};

// .spk-scan — multiroom scan button: the dark variant of .update-btn. Its label
// is --bg (not #fff) so it inverts with the theme: dark button + light label in
// light mode, light button + dark label in dark mode.
const ScanButton = styled(Button).attrs({ $variant: 'update' })`
  background: var(--ink);
  color: var(--bg);
  &:hover { background: var(--ink-2); }
`;

// Shown once an update lands: a blocking full-screen overlay that forces the
// page to reload onto the new bundle. Sits above the sheet (z-index 50) so the
// stale app can't be used in the meantime.
const UpdateOverlay = styled.div`
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(31, 24, 20, 0.45);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
`;

const UpdateCard = styled.div`
  background: var(--surface);
  border-radius: 22px;
  padding: 30px 26px;
  max-width: 320px;
  width: 100%;
  text-align: center;
  box-shadow: 0 30px 80px -20px rgba(31, 24, 20, 0.35);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
`;

const UpdateTitle = styled.div`
  font-size: 17px;
  font-weight: 700;
  color: var(--ink);
`;

const UpdateSub = styled.div`
  font-size: 14px;
  color: var(--ink-3);
`;

// Root settings menu: one tappable row per category, each opening its own
// subpage. Styled to sit on the same card as the form fields it replaces.
const MenuItem = styled.button`
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  padding: 14px 0;
  text-align: left;
  background: transparent;
  border: none;
  cursor: pointer;
  color: var(--ink);

  & + & { border-top: 1px solid rgba(31, 24, 20, 0.07); }
`;

const MenuIcon = styled.span`
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: var(--accent-soft);
  color: var(--accent);
  display: grid;
  place-items: center;
  flex-shrink: 0;
`;

const MenuLabel = styled.span`
  flex: 1;
  min-width: 0;
  font-size: 15px;
  font-weight: 600;
`;

const MenuChev = styled.span`
  color: var(--ink-3);
  display: grid;
  place-items: center;
  flex-shrink: 0;
`;

// Multiroom: group other SoundTouch speakers on the network with this one (this
// radio is the zone master; the rest follow it via Bose's native zone API). The
// list comes from a network sweep, so it auto-scans on open and offers a manual
// rescan. Each row toggles membership; toggles apply optimistically and revert
// on failure.
function MultiroomSection({ open }) {
  const { t } = useI18n();
  const [speakers, setSpeakers] = useState(null); // null = not scanned yet; [] = scanned, none found
  const [scanning, setScanning] = useState(false);
  const [busy, setBusy] = useState({}); // ip -> in-flight toggle
  const scanGen = useRef(0); // bumped per scan so a slow one can't clobber a newer

  const scan = useCallback(async () => {
    const gen = ++scanGen.current;
    setScanning(true);
    const list = await findSpeakers();
    if (gen !== scanGen.current) return; // superseded by a newer scan (or sheet closed)
    setSpeakers(list);
    setScanning(false);
  }, []);

  // Scan when the sheet opens (not at app start — the sheet is always mounted,
  // just hidden), so the list is fresh each time the user actually looks at it.
  // Cleanup bumps the generation so an in-flight scan from a now-closed sheet is
  // discarded instead of landing on top of the next open's results.
  useEffect(() => {
    if (open) scan();
    return () => { scanGen.current += 1; };
  }, [open, scan]);

  const toggle = async (sp) => {
    if (busy[sp.ip]) return;
    const want = !sp.grouped;
    setBusy((b) => ({ ...b, [sp.ip]: true }));
    setSpeakers((list) => list.map((x) => (x.ip === sp.ip ? { ...x, grouped: want } : x)));
    try {
      await (want ? groupSpeaker(sp.ip) : ungroupSpeaker(sp.ip));
    } catch {
      setSpeakers((list) => list.map((x) => (x.ip === sp.ip ? { ...x, grouped: !want } : x)));
    } finally {
      setBusy((b) => ({ ...b, [sp.ip]: false }));
    }
  };

  return (
    <>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('multiroomHint')}</FieldHint>
      {speakers && speakers.length > 0 && (
        <FieldCard>
          {speakers.map((sp) => (
            <SpeakerRow
              key={sp.deviceId || sp.ip}
              speaker={sp}
              disabled={!!busy[sp.ip]}
              onToggle={() => toggle(sp)}
            />
          ))}
        </FieldCard>
      )}
      {speakers && speakers.length === 0 && !scanning && (
        <FieldHint>{t('noSpeakers')}</FieldHint>
      )}
      <ScanButton onClick={scan} disabled={scanning}>
        {scanning ? <Spinner $scan /> : <Icon.search width="18" height="18" />}
        <span>{t('findSpeakers')}</span>
      </ScanButton>
    </>
  );
}

// MQTT / Home Assistant: connect this speaker to a broker so it appears in Home
// Assistant (via the on-box bridge, internal/habridge). The form is self-contained
// — it loads the stored config from /api/settings, saves the whole block on demand
// (a reconnect churns the broker link, so it's a deliberate action, not per-key),
// and polls the live connection status while enabled. The password is never sent
// back to the browser, so an empty password field keeps the stored one.
function MqttSection({ open, onAuthError }) {
  const { t } = useI18n();
  const [cfg, setCfg] = useState(null);        // null until loaded
  const [hasPassword, setHasPassword] = useState(false);
  const [status, setStatus] = useState({ connected: false, lastError: '' });
  const [saving, setSaving] = useState(false);
  const pollRef = useRef(null);

  useEffect(() => {
    if (!open) return;
    getSettings().then((s) => {
      const m = (s && s.mqtt) || {};
      setCfg({
        enabled: !!m.enabled,
        host: m.host || '',
        port: m.port || 1883,
        username: m.username || '',
        password: '',
        baseTopic: m.baseTopic || '',
        discoveryPrefix: m.discoveryPrefix || 'homeassistant',
        tls: !!m.tls,
      });
      setHasPassword(!!m.hasPassword);
      setStatus({ connected: !!m.connected, lastError: m.lastError || '' });
    });
  }, [open]);

  // Poll the live broker status while the section is open and enabled.
  useEffect(() => {
    clearInterval(pollRef.current);
    if (!open || !cfg?.enabled) return undefined;
    pollRef.current = setInterval(async () => {
      const s = await getMqttStatus();
      if (s) setStatus(s);
    }, 4000);
    return () => clearInterval(pollRef.current);
  }, [open, cfg?.enabled]);

  if (!cfg) return null;
  const set = (k, v) => setCfg((c) => ({ ...c, [k]: v }));

  const save = async () => {
    setSaving(true);
    const patch = {
      enabled: cfg.enabled,
      host: cfg.host.trim(),
      port: Number(cfg.port) || 0,
      username: cfg.username,
      baseTopic: cfg.baseTopic.trim(),
      discoveryPrefix: cfg.discoveryPrefix.trim(),
      tls: cfg.tls,
    };
    if (cfg.password) patch.password = cfg.password; // blank keeps the stored one
    try {
      await saveSettings({ mqtt: patch });
      if (cfg.password) setHasPassword(true);
      set('password', '');
      setTimeout(async () => { const s = await getMqttStatus(); if (s) setStatus(s); }, 1200);
    } catch (e) {
      if (e?.status === 401) onAuthError?.(); // session expired mid-edit
    } finally {
      setSaving(false);
    }
  };

  const dot = {
    display: 'inline-block', width: 8, height: 8, borderRadius: 99,
    marginRight: 6, verticalAlign: 'middle',
    background: status.connected ? '#2ecc71' : 'var(--muted, #9aa0a6)',
  };

  return (
    <>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('mqttHint')}</FieldHint>
      <FieldCard>
        <FieldRow>
          <FieldRowLabel as="span">{t('mqttEnable')}</FieldRowLabel>
          <Toggle on={cfg.enabled} onClick={() => set('enabled', !cfg.enabled)} aria-label={t('mqttEnable')} style={{ marginLeft: 'auto' }} />
        </FieldRow>
      </FieldCard>
      {cfg.enabled && (
        <>
          <FieldCard style={{ marginTop: 12 }}>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-host">{t('mqttHost')}</FieldRowLabel>
              <FieldRowInput id="mqtt-host" type="text" value={cfg.host} onChange={(e) => set('host', e.target.value)} placeholder="192.168.1.10" autoComplete="off" />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-port">{t('mqttPort')}</FieldRowLabel>
              <FieldRowInput id="mqtt-port" type="number" value={cfg.port} onChange={(e) => set('port', e.target.value)} placeholder="1883" />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-user">{t('mqttUsername')}</FieldRowLabel>
              <FieldRowInput id="mqtt-user" type="text" value={cfg.username} onChange={(e) => set('username', e.target.value)} autoComplete="off" />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-pass">{t('mqttPassword')}</FieldRowLabel>
              <FieldRowInput id="mqtt-pass" type="password" value={cfg.password} onChange={(e) => set('password', e.target.value)} placeholder={hasPassword ? '••••••••' : ''} autoComplete="new-password" />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel as="span">{t('mqttTls')}</FieldRowLabel>
              <Toggle on={cfg.tls} onClick={() => set('tls', !cfg.tls)} aria-label={t('mqttTls')} style={{ marginLeft: 'auto' }} />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-base">{t('mqttBaseTopic')}</FieldRowLabel>
              <FieldRowInput id="mqtt-base" type="text" value={cfg.baseTopic} onChange={(e) => set('baseTopic', e.target.value)} placeholder="retouch/…" autoComplete="off" />
            </FieldRow>
            <FieldRow>
              <FieldRowLabel htmlFor="mqtt-disc">{t('mqttDiscoveryPrefix')}</FieldRowLabel>
              <FieldRowInput id="mqtt-disc" type="text" value={cfg.discoveryPrefix} onChange={(e) => set('discoveryPrefix', e.target.value)} placeholder="homeassistant" autoComplete="off" />
            </FieldRow>
          </FieldCard>
          {hasPassword && !cfg.password && <FieldHint>{t('mqttPasswordSet')}</FieldHint>}
          <Button $variant="update" onClick={save} disabled={saving || !cfg.host.trim()} style={{ marginTop: 8 }}>
            {saving ? <Spinner $scan /> : <Icon.check width="18" height="18" />}
            <span>{t('mqttSave')}</span>
          </Button>
          <FieldHint style={{ marginTop: 8 }}>
            <span style={dot} />
            {status.connected ? t('mqttConnected') : t('mqttDisconnected')}
            {status.lastError ? ` — ${status.lastError}` : ''}
          </FieldHint>
        </>
      )}
    </>
  );
}

// Login gate shown instead of the settings when a password is set and this
// browser has no (valid) session. The forgot-password path is deliberately not
// a form: a factory reset of the speaker clears the password (physical access
// is the proof of ownership), which the hint below explains.
function LoginView({ onUnlocked }) {
  const { t } = useI18n();
  const [pw, setPw] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);

  const submit = async (e) => {
    e.preventDefault();
    if (busy || !pw) return;
    setBusy(true);
    setError(false);
    try {
      await login(pw);
      onUnlocked();
    } catch {
      setError(true);
      setPw('');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Form as="form" onSubmit={submit}>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('loginHint')}</FieldHint>
      <FieldCard>
        <FieldRow>
          <FieldRowLabel htmlFor="login-pass">{t('password')}</FieldRowLabel>
          <FieldRowInput
            id="login-pass"
            type="password"
            value={pw}
            onChange={(e) => setPw(e.target.value)}
            autoComplete="current-password"
            autoFocus
          />
        </FieldRow>
      </FieldCard>
      {error && <FieldHint $error>{t('wrongPassword')}</FieldHint>}
      <Button $variant="update" type="submit" disabled={busy || !pw} style={{ marginTop: 8 }}>
        {busy ? <Spinner $scan /> : <Icon.shield width="18" height="18" />}
        <span>{t('login')}</span>
      </Button>
      <FieldHint style={{ marginTop: 10 }}>{t('forgotPasswordHint')}</FieldHint>
    </Form>
  );
}

// Set / change the settings password (security page). Setting the first
// password locks the settings for everyone else on the network; the server
// hands this browser a session so the user isn't locked out of the page they
// are looking at.
function PasswordSection({ hasPassword, onChanged, onAuthError }) {
  const { t } = useI18n();
  const [current, setCurrent] = useState('');
  const [pw1, setPw1] = useState('');
  const [pw2, setPw2] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState(null); // { error, text }

  const submit = async () => {
    if (busy) return;
    if (pw1.trim().length < 4) { setMsg({ error: true, text: t('passwordTooShort') }); return; }
    if (pw1 !== pw2) { setMsg({ error: true, text: t('passwordMismatch') }); return; }
    setBusy(true);
    setMsg(null);
    try {
      await setPassword({ currentPassword: current, newPassword: pw1 });
      setCurrent(''); setPw1(''); setPw2('');
      setMsg({ error: false, text: t('passwordSaved') });
      onChanged();
    } catch (e) {
      // 401 with a live form means the current password was wrong — unless the
      // session itself expired ("login required"), which sends us back to login.
      if (e?.status === 401 && e.message === 'login required') { onAuthError?.(); return; }
      setMsg({ error: true, text: e?.status === 401 ? t('wrongPassword') : t('passwordSaveError') });
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <FormSection>{hasPassword ? t('changePassword') : t('setPassword')}</FormSection>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('passwordHint')}</FieldHint>
      <FieldCard>
        {hasPassword && (
          <FieldRow>
            <FieldRowLabel htmlFor="pw-current">{t('currentPassword')}</FieldRowLabel>
            <FieldRowInput id="pw-current" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
          </FieldRow>
        )}
        <FieldRow>
          <FieldRowLabel htmlFor="pw-new">{t('newPassword')}</FieldRowLabel>
          <FieldRowInput id="pw-new" type="password" value={pw1} onChange={(e) => setPw1(e.target.value)} autoComplete="new-password" />
        </FieldRow>
        <FieldRow>
          <FieldRowLabel htmlFor="pw-confirm">{t('confirmPassword')}</FieldRowLabel>
          <FieldRowInput id="pw-confirm" type="password" value={pw2} onChange={(e) => setPw2(e.target.value)} autoComplete="new-password" />
        </FieldRow>
      </FieldCard>
      {msg && <FieldHint $error={msg.error}>{msg.text}</FieldHint>}
      <Button
        $variant="update"
        onClick={submit}
        disabled={busy || !pw1 || (hasPassword && !current)}
        style={{ marginTop: 8 }}
      >
        {busy ? <Spinner $scan /> : <Icon.shield width="18" height="18" />}
        <span>{hasPassword ? t('changePassword') : t('setPassword')}</span>
      </Button>
      {!hasPassword && <FieldHint style={{ marginTop: 10 }}>{t('forgotPasswordHint')}</FieldHint>}
    </>
  );
}

// Shimmering placeholder shown while the sheet's first settings fetch is in
// flight. Mirrors the real form's section rhythm so the layout doesn't jump
// when the data lands.
// Mirrors the category menu the sheet opens on: a card of icon + label + chevron rows.
function SettingsSkeleton() {
  const labelWidths = ['38%', '30%', '44%', '34%', '48%', '40%'];
  return (
    <Form aria-hidden="true">
      <FieldCard>
        {labelWidths.map((w, i) => (
          <MenuItem as="div" key={i} style={{ cursor: 'default' }}>
            <Skeleton style={{ width: 34, height: 34 }} $radius="11px" />
            <Skeleton style={{ width: w, height: 14 }} $radius="6px" />
            <Skeleton style={{ width: 20, height: 20, marginLeft: 'auto' }} $radius="6px" />
          </MenuItem>
        ))}
      </FieldCard>
    </Form>
  );
}

// Settings sheet: speaker name + sound (bass, and treble where the speaker has
// tone controls), UI language (persisted locally), and device-specific network
// settings (Wi-Fi/streaming optimization + a read-only connection summary). Each
// device setting only appears when the speaker actually reports it. Live-applies
// every field.
export function SettingsSheet({ open, onClose, lang, onSetLang, themeMode, onSetTheme, onNameChange }) {
  const { t } = useI18n();
  const [name, setName] = useState('');
  const [bass, setBass] = useState(0);
  const [caps, setCaps] = useState({ min: -9, max: 0, default: 0 });
  // Device-specific settings, only shown when the speaker reports them.
  const [treble, setTreble] = useState(null);            // null = unsupported/hidden
  const [trebleCaps, setTrebleCaps] = useState({ min: -100, max: 100, step: 10 });
  const [wifiOpt, setWifiOpt] = useState(null);          // null = unsupported/hidden
  const [closeTelnet, setCloseTelnet] = useState(false);
  const [network, setNetwork] = useState(null);          // { type, ssid, signal, ip }
  const [ver, setVer] = useState(null);                  // { version, updatable }
  const [betas, setBetas] = useState([]);                // open-PR beta builds
  const [showBetas, setShowBetas] = useState(() => localStorage.getItem(betaUpdatesKey) === '1');
  const [selTag, setSelTag] = useState('');              // '' = latest stable
  const [upd, setUpd] = useState({ phase: 'idle', text: '' }); // idle | busy | done | error
  const [loading, setLoading] = useState(true); // true until the first settings fetch resolves
  const [page, setPage] = useState(null); // null = category menu; else the open subpage key
  const [pluginPage, setPluginPage] = useState(null); // selected plugin object within the plugins subpage
  const [auth, setAuth] = useState(null); // { hasPassword, authenticated } | null = unknown
  const nameTimer = useRef(null);
  const pollRef = useRef(null);
  const pollGen = useRef(0); // bumped to invalidate a poll tick that is mid-await

  // A password is set and this browser has no session: show the login gate.
  const locked = !!auth && auth.hasPassword && !auth.authenticated;

  const loadAll = useCallback(() => {
    getSettings().then((s) => {
      setLoading(false);
      if (!s) return;
      if (typeof s.name === 'string') setName(s.name);
      if (s.bass) {
        setBass(s.bass.actual ?? 0);
        setCaps({ min: s.bass.min ?? -9, max: s.bass.max ?? 0, default: s.bass.default ?? 0 });
      }
      if (s.treble) {
        setTreble(s.treble.value ?? 0);
        setTrebleCaps({ min: s.treble.min ?? -100, max: s.treble.max ?? 100, step: s.treble.step || 1 });
      } else setTreble(null);
      setWifiOpt(typeof s.wifiOptimization === 'boolean' ? s.wifiOptimization : null);
      setCloseTelnet(!!s.closeTelnet);
      setNetwork(s.network || null);
    }).catch(() => setLoading(false));
    getVersion().then((v) => v && setVer(v));
    getReleases().then((r) => { if (r) setBetas(r.betas || []); });
  }, []);

  useEffect(() => {
    if (!open) { setLoading(true); setPage(null); setPluginPage(null); setAuth(null); return; }
    // Auth first: when the settings are locked, don't load (the API would only
    // return the restricted view anyway) — show the login gate instead.
    getAuth().then((a) => {
      const cur = a || { hasPassword: false, authenticated: true };
      setAuth(cur);
      if (cur.hasPassword && !cur.authenticated) { setLoading(false); return; }
      loadAll();
    });
  }, [open, loadAll]);

  // Leaving the plugins category drops any plugin the user had drilled into, so
  // reopening the category lands back on the list rather than a stale detail view.
  useEffect(() => { if (page !== 'plugins') setPluginPage(null); }, [page]);

  // A 401 on any live-apply call means the session expired (or the password was
  // set from another browser): drop back to the login gate.
  const onAuthExpired = useCallback(() => {
    setAuth({ hasPassword: true, authenticated: false });
    setPage(null);
  }, []);
  const saveGuarded = useCallback((patch) => {
    saveSettings(patch).catch((e) => { if (e?.status === 401) onAuthExpired(); });
  }, [onAuthExpired]);

  // Stop any version poll when the sheet closes or unmounts. Bumping pollGen
  // also cancels a tick that is mid-await (clearTimeout alone can't stop that:
  // the resumed tick would re-arm the timer and setState behind a closed sheet).
  useEffect(() => {
    if (open) return;
    pollGen.current += 1;
    clearTimeout(pollRef.current);
    clearTimeout(nameTimer.current); // don't PUT a half-typed name after close
    setUpd({ phase: 'idle', text: '' });
  }, [open]);
  useEffect(() => () => {
    pollGen.current += 1;
    clearTimeout(pollRef.current);
    clearTimeout(nameTimer.current);
  }, []);

  // Poll /api/version until the speaker comes back on the target tag (it restarts
  // mid-update, so the endpoint drops out for a bit). Times out after ~3 minutes.
  const pollVersion = (target) => {
    const startV = ver?.version;
    const gen = ++pollGen.current;
    let n = 0;
    const tick = async () => {
      n += 1;
      const v = await getVersion();
      if (gen !== pollGen.current) return; // sheet closed / poll superseded
      // Done when we reach the target tag, or — with no known target — when the
      // version changes from a KNOWN baseline. Without a baseline we can't tell a
      // change from a first read, so keep polling rather than declare success.
      const changed = startV != null && v?.version && v.version !== startV;
      if (v?.version && (target ? v.version === target : changed)) {
        setVer(v);
        // The new build is now being served, but this page still runs the old
        // bundle — surface a reload so the user picks up the update.
        setUpd({ phase: 'updated', text: `${t('updateDone')} ${v.version}` });
        return;
      }
      if (n >= 45) { setUpd({ phase: 'error', text: t('updateError') }); return; }
      pollRef.current = setTimeout(tick, 4000);
    };
    pollRef.current = setTimeout(tick, 6000);
  };

  const onUpdate = async () => {
    if (upd.phase === 'busy') return;
    setUpd({ phase: 'busy', text: t('updating') });
    let res;
    try { res = await startUpdate(selTag || undefined); } catch { setUpd({ phase: 'error', text: t('updateError') }); return; }
    if (res.status === 401) { setUpd({ phase: 'idle', text: '' }); onAuthExpired(); return; }
    if (res.status === 200 && res.body.status === 'current') {
      setUpd({ phase: 'done', text: t('upToDate') });
      return;
    }
    if (res.body.status === 'updating' || res.status === 202) {
      pollVersion(res.body.to || null);
      return;
    }
    setUpd({ phase: 'error', text: t('updateError') });
  };

  const onShowBetas = () => {
    const next = !showBetas;
    setShowBetas(next);
    localStorage.setItem(betaUpdatesKey, next ? '1' : '0');
    setSelTag(next ? (betas[0]?.tag || '') : '');
  };

  // A finished update leaves this page running the old bundle, so a reload is
  // mandatory. Trigger it automatically once the new version is being served;
  // the overlay blocks the stale UI until the fresh page loads.
  useEffect(() => {
    if (upd.phase !== 'updated') return undefined;
    const id = setTimeout(() => window.location.reload(), 2200);
    return () => clearTimeout(id);
  }, [upd.phase]);

  const updateLabel = selTag ? t('installSelected') : (ver?.version?.startsWith('beta-') ? t('installStable') : t('updateNow'));

  useEffect(() => {
    if (!open) return;
    const f = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', f);
    return () => window.removeEventListener('keydown', f);
  }, [open, onClose]);

  const onNameInput = (v) => {
    setName(v);
    clearTimeout(nameTimer.current);
    nameTimer.current = setTimeout(() => {
      const nm = v.trim();
      if (nm) { saveGuarded({ name: nm }); onNameChange && onNameChange(nm); }
    }, 600);
  };

  const onBass = (v) => { setBass(v); saveGuarded({ bass: v }); };

  // Treble snaps to the step the speaker accepts.
  const onTreble = (v) => {
    const step = trebleCaps.step || 1;
    const snapped = Math.round(v / step) * step;
    setTreble(snapped);
    saveGuarded({ treble: snapped });
  };

  const onWifiOpt = () => {
    const next = !wifiOpt;
    setWifiOpt(next);
    saveGuarded({ wifiOptimization: next });
  };

  const onCloseTelnet = async () => {
    const next = !closeTelnet;
    setCloseTelnet(next);
    try {
      await saveSettings({ closeTelnet: next });
    } catch (e) {
      setCloseTelnet(!next);
      if (e?.status === 401) onAuthExpired();
    }
  };

  // Login succeeded (or the first password was just set): load the real
  // settings this browser couldn't see while locked.
  const onUnlocked = useCallback(() => {
    setAuth({ hasPassword: true, authenticated: true });
    setLoading(true);
    loadAll();
  }, [loadAll]);

  const onLogout = async () => {
    try { await logout(); } catch { /* the cookie may already be gone */ }
    setAuth({ hasPassword: true, authenticated: false });
    setPage(null);
  };

  // Categories shown on the root menu. Network and Software only appear when the
  // speaker actually reports the data those pages need.
  const categories = [
    { key: 'general', icon: Icon.speaker },
    { key: 'sound', icon: Icon.volume },
    (wifiOpt !== null || network) && { key: 'network', icon: Icon.wifi },
    { key: 'multiroom', icon: Icon.layers },
    { key: 'mqtt', icon: Icon.globe },
    { key: 'plugins', icon: Icon.settings },
    { key: 'security', icon: Icon.shield },
    ver && { key: 'software', icon: Icon.download },
  ].filter(Boolean);

  // Body for each subpage. Only the open page's element is rendered into the tree.
  const pages = {
    general: (
      <>
        <FormSection>{t('name')}</FormSection>
        <FieldCard>
          <FieldRow>
            <FieldRowLabel htmlFor="set-name">{t('name')}</FieldRowLabel>
            <FieldRowInput
              id="set-name"
              type="text"
              value={name}
              onChange={(e) => onNameInput(e.target.value)}
              placeholder={t('namePlaceholder')}
              maxLength={28}
              autoComplete="off"
            />
          </FieldRow>
        </FieldCard>

        <FormSection style={{ marginTop: 22 }}>{t('language')}</FormSection>
        <SelectWrap>
          <Select
            value={lang}
            onChange={(e) => onSetLang(e.target.value)}
            aria-label={t('language')}
          >
            {LANGS.map((l) => <option key={l.code} value={l.code}>{l.label}</option>)}
          </Select>
          <SelectChev aria-hidden="true"><Icon.chevron width="18" height="18" /></SelectChev>
        </SelectWrap>

        <FormSection style={{ marginTop: 22 }}>{t('appearance')}</FormSection>
        <SelectWrap>
          <Select
            value={themeMode}
            onChange={(e) => onSetTheme(e.target.value)}
            aria-label={t('appearance')}
          >
            <option value="system">{t('themeSystem')}</option>
            <option value="light">{t('themeLight')}</option>
            <option value="dark">{t('themeDark')}</option>
          </Select>
          <SelectChev aria-hidden="true"><Icon.chevron width="18" height="18" /></SelectChev>
        </SelectWrap>
      </>
    ),
    sound: (
      <>
        <BassCard>
          <BassHead>
            <BassName>{t('bass')}</BassName>
            <BassVal $set={bass !== caps.default}>{fmtBass(bass)}</BassVal>
          </BassHead>
          <BassSlider value={bass} min={caps.min} max={caps.max} origin={caps.default} onChange={onBass} />
          <BassScale>
            <span>{fmtBass(caps.min)}</span>
            <span>{fmtBass(caps.max)}</span>
          </BassScale>
        </BassCard>
        <FieldHint>{t('bassHint')}</FieldHint>

        {treble !== null && (
          <>
            <BassCard style={{ marginTop: 12 }}>
              <BassHead>
                <BassName>{t('treble')}</BassName>
                <BassVal $set={treble !== 0}>{fmtBass(treble)}</BassVal>
              </BassHead>
              <BassSlider value={treble} min={trebleCaps.min} max={trebleCaps.max} origin={0} onChange={onTreble} />
              <BassScale>
                <span>{fmtBass(trebleCaps.min)}</span>
                <span>{fmtBass(trebleCaps.max)}</span>
              </BassScale>
            </BassCard>
            <FieldHint>{t('trebleHint')}</FieldHint>
          </>
        )}
      </>
    ),
    network: (
      <>
        {wifiOpt !== null && (
          <>
            <FieldCard>
              <FieldRow>
                <FieldRowLabel as="span">{t('wifiOptimization')}</FieldRowLabel>
                <Toggle
                  on={wifiOpt}
                  onClick={onWifiOpt}
                  aria-label={t('wifiOptimization')}
                  style={{ marginLeft: 'auto' }}
                />
              </FieldRow>
            </FieldCard>
            <FieldHint>{t('wifiOptimizationHint')}</FieldHint>
          </>
        )}
        {network && (
          <FieldCard style={{ marginTop: wifiOpt !== null ? 12 : 0 }}>
            {network.ssid && (
              <FieldRow>
                <FieldRowLabel as="span">{t('wifiNetwork')}</FieldRowLabel>
                <FieldRowValue>{network.ssid}</FieldRowValue>
              </FieldRow>
            )}
            {network.signal && (
              <FieldRow>
                <FieldRowLabel as="span">{t('signal')}</FieldRowLabel>
                <FieldRowValue>{sigLabel(t, network.signal)}</FieldRowValue>
              </FieldRow>
            )}
            {network.ip && (
              <FieldRow>
                <FieldRowLabel as="span">{t('ipAddress')}</FieldRowLabel>
                <FieldRowValue>{network.ip}</FieldRowValue>
              </FieldRow>
            )}
          </FieldCard>
        )}
      </>
    ),
    multiroom: <MultiroomSection open={open && page === 'multiroom'} />,
    mqtt: <MqttSection open={open && page === 'mqtt'} onAuthError={onAuthExpired} />,
    security: (
      <>
        <PasswordSection
          hasPassword={!!auth?.hasPassword}
          onChanged={() => setAuth({ hasPassword: true, authenticated: true })}
          onAuthError={onAuthExpired}
        />
        <FormSection style={{ marginTop: 22 }}>{t('closeTelnet')}</FormSection>
        <FieldCard>
          <FieldRow>
            <FieldRowLabel as="span">{t('closeTelnet')}</FieldRowLabel>
            <Toggle
              on={closeTelnet}
              onClick={onCloseTelnet}
              aria-label={t('closeTelnet')}
              style={{ marginLeft: 'auto' }}
            />
          </FieldRow>
        </FieldCard>
        <FieldHint>{t('closeTelnetHint')}</FieldHint>
        {auth?.hasPassword && (
          <Button $variant="update" onClick={onLogout} style={{ marginTop: 22 }}>
            <Icon.shield width="18" height="18" />
            <span>{t('logout')}</span>
          </Button>
        )}
      </>
    ),
    plugins: pluginPage
      ? <PluginSettings p={pluginPage} onRemoved={() => setPluginPage(null)} />
      : <PluginsSection open={open && page === 'plugins'} onOpen={setPluginPage} />,
    software: ver && (
      <>
        <FieldCard>
          <FieldRow>
            <FieldRowLabel as="span">{t('version')}</FieldRowLabel>
            <FieldRowValue>{ver.version}</FieldRowValue>
          </FieldRow>
        </FieldCard>
        {ver.updatable ? (
          <>
            {betas.length > 0 && (
              <FieldCard style={{ marginTop: 8, marginBottom: 8 }}>
                <FieldRow>
                  <FieldRowLabel as="span">{t('betaUpdates')}</FieldRowLabel>
                  <Toggle
                    on={showBetas}
                    onClick={onShowBetas}
                    aria-label={t('betaUpdates')}
                    style={{ marginLeft: 'auto' }}
                  />
                </FieldRow>
              </FieldCard>
            )}
            {showBetas && betas.length > 0 && (
              <>
                <SelectWrap style={{ marginBottom: 8 }}>
                  <Select
                    value={selTag}
                    onChange={(e) => setSelTag(e.target.value)}
                    disabled={upd.phase === 'busy'}
                    aria-label={t('chooseBetaVersion')}
                  >
                    {betas.map((b) => (
                      <option key={b.tag} value={b.tag}>{b.name}</option>
                    ))}
                  </Select>
                  <SelectChev aria-hidden="true"><Icon.chevron width="18" height="18" /></SelectChev>
                </SelectWrap>
                <FieldHint style={{ marginBottom: 8 }}>{t('betaUpdatesHint')}</FieldHint>
              </>
            )}
            <Button $variant="update" onClick={onUpdate} disabled={upd.phase === 'busy'}>
              <Icon.download width="18" height="18" />
              <span>{updateLabel}</span>
            </Button>
            {upd.text && upd.phase !== 'updated' && (
              <FieldHint $error={upd.phase === 'error'}>{upd.text}</FieldHint>
            )}
          </>
        ) : (
          <FieldHint>{t('updatesOnSpeaker')}</FieldHint>
        )}
      </>
    ),
  };

  // The header arrow walks the stack back one level: plugin detail → plugins list
  // → category menu → close.
  const onBack = pluginPage
    ? () => setPluginPage(null)
    : page
      ? () => setPage(null)
      : onClose;

  return (
    <>
      <SheetScrim $open={open} onClick={onClose} />
      <SheetEl $open={open} role="dialog" aria-modal="true">
        <SheetHandle />
        <SheetHeader
          onClose={onBack}
          closeLabel={(page || pluginPage) ? t('back') : t('close')}
          headline={pluginPage ? pluginPage.title : page ? t(page) : t('settings')}
        >
          <SetEyebrow>{pluginPage ? t('plugins') : page ? t('settings') : t('thisRadio')}</SetEyebrow>
        </SheetHeader>
        <SheetBody>
          {loading ? (
            <SettingsSkeleton />
          ) : locked ? (
            <LoginView onUnlocked={onUnlocked} />
          ) : page ? (
            <Form>{pages[page]}</Form>
          ) : (
            <FieldCard>
              {categories.map((c) => (
                <MenuItem key={c.key} onClick={() => setPage(c.key)}>
                  <MenuIcon aria-hidden="true"><c.icon width="19" height="19" /></MenuIcon>
                  <MenuLabel>{t(c.key)}</MenuLabel>
                  <MenuChev aria-hidden="true"><Icon.chevron width="20" height="20" /></MenuChev>
                </MenuItem>
              ))}
            </FieldCard>
          )}
        </SheetBody>
      </SheetEl>
      {upd.phase === 'updated' && (
        <UpdateOverlay role="alertdialog" aria-modal="true" onClick={() => window.location.reload()}>
          <UpdateCard>
            <Spinner $scan />
            <UpdateTitle>{upd.text}</UpdateTitle>
            <UpdateSub>{t('updateReloading')}</UpdateSub>
          </UpdateCard>
        </UpdateOverlay>
      )}
    </>
  );
}

export default SettingsSheet;
