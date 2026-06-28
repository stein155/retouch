import { useState, useEffect, useRef, useCallback } from 'react';
import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { Spinner } from '../atoms/Spinner';
import { Button } from '../atoms/Button';
import { Toggle } from '../atoms/Toggle';
import { BassSlider } from '../molecules/BassSlider';
import { SpeakerRow } from '../molecules/SpeakerRow';
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
} from '../../lib/api';

const fmtBass = (v) => (v > 0 ? '+' + v : String(v));
const betaUpdatesKey = 'retouch-beta-updates';

// Map the speaker's signal token to a localised label.
const sigLabel = (t, sig) => {
  const key = { excellent: 'sigExcellent', good: 'sigGood', fair: 'sigFair', poor: 'sigPoor' }[sig];
  return key ? t(key) : sig;
};

// .spk-scan — multiroom scan button: the dark variant of .update-btn.
const ScanButton = styled(Button).attrs({ $variant: 'update' })`
  background: var(--ink);
  &:hover { background: var(--ink-2); }
`;

// Multiroom: group other SoundTouch speakers on the network with this one (this
// radio is the zone master; the rest follow it via Bose's native zone API). The
// list comes from a network sweep, so it auto-scans on open and offers a manual
// rescan. Each row toggles membership; toggles apply optimistically and revert
// on failure.
function MultiroomSection() {
  const { t } = useI18n();
  const [speakers, setSpeakers] = useState(null); // null = not scanned yet; [] = scanned, none found
  const [scanning, setScanning] = useState(false);
  const [busy, setBusy] = useState({}); // ip -> in-flight toggle

  const scan = useCallback(async () => {
    setScanning(true);
    const list = await findSpeakers();
    setSpeakers(list);
    setScanning(false);
  }, []);

  useEffect(() => { scan(); }, [scan]);

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
      <FormSection style={{ marginTop: 22 }}>{t('multiroom')}</FormSection>
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

// Settings sheet: speaker name + sound (bass, and treble where the speaker has
// tone controls), UI language (persisted locally), and device-specific network
// settings (Wi-Fi/streaming optimization + a read-only connection summary). Each
// device setting only appears when the speaker actually reports it. Live-applies
// every field.
export function SettingsSheet({ open, onClose, lang, onSetLang, onNameChange }) {
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
  const [host, setHost] = useState('');                  // friendly .local address
  const [ver, setVer] = useState(null);                  // { version, updatable }
  const [betas, setBetas] = useState([]);                // open-PR beta builds
  const [showBetas, setShowBetas] = useState(() => localStorage.getItem(betaUpdatesKey) === '1');
  const [selTag, setSelTag] = useState('');              // '' = latest stable
  const [upd, setUpd] = useState({ phase: 'idle', text: '' }); // idle | busy | done | error
  const nameTimer = useRef(null);
  const pollRef = useRef(null);

  useEffect(() => {
    if (!open) return;
    getSettings().then((s) => {
      if (!s) return;
      if (typeof s.name === 'string') setName(s.name);
      if (typeof s.host === 'string') setHost(s.host);
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
    });
    getVersion().then((v) => v && setVer(v));
    getReleases().then((r) => { if (r) setBetas(r.betas || []); });
  }, [open]);

  // Stop any version poll when the sheet closes or unmounts.
  useEffect(() => {
    if (open) return;
    clearTimeout(pollRef.current);
    setUpd({ phase: 'idle', text: '' });
  }, [open]);
  useEffect(() => () => clearTimeout(pollRef.current), []);

  // Poll /api/version until the speaker comes back on the target tag (it restarts
  // mid-update, so the endpoint drops out for a bit). Times out after ~3 minutes.
  const pollVersion = (target) => {
    const startV = ver?.version;
    let n = 0;
    const tick = async () => {
      n += 1;
      const v = await getVersion();
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
      if (nm) { saveSettings({ name: nm }); onNameChange && onNameChange(nm); }
    }, 600);
  };

  const onBass = (v) => { setBass(v); saveSettings({ bass: v }); };

  // Treble snaps to the step the speaker accepts.
  const onTreble = (v) => {
    const step = trebleCaps.step || 1;
    const snapped = Math.round(v / step) * step;
    setTreble(snapped);
    saveSettings({ treble: snapped });
  };

  const onWifiOpt = () => {
    const next = !wifiOpt;
    setWifiOpt(next);
    saveSettings({ wifiOptimization: next });
  };

  const onCloseTelnet = async () => {
    const next = !closeTelnet;
    setCloseTelnet(next);
    try { await saveSettings({ closeTelnet: next }); } catch { setCloseTelnet(!next); }
  };

  return (
    <>
      <SheetScrim $open={open} onClick={onClose} />
      <SheetEl $open={open} role="dialog" aria-modal="true">
        <SheetHandle />
        <SheetHeader onClose={onClose} closeLabel={t('close')} headline={t('settings')}>
          <SetEyebrow>{t('thisRadio')}</SetEyebrow>
        </SheetHeader>
        <SheetBody>
          <Form>
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
            {host && (
              <FieldHint>{t('reachableAt')} <b>{host}</b></FieldHint>
            )}

            <FormSection style={{ marginTop: 22 }}>{t('sound')}</FormSection>
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

            {(wifiOpt !== null || network) && (
              <>
                <FormSection style={{ marginTop: 22 }}>{t('network')}</FormSection>
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
            )}

            <FormSection style={{ marginTop: 22 }}>{t('security')}</FormSection>
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

            <MultiroomSection />

            {ver && (
              <>
                <FormSection style={{ marginTop: 22 }}>{t('software')}</FormSection>
                <FieldCard>
                  <FieldRow>
                    <FieldRowLabel as="span">{t('version')}</FieldRowLabel>
                    <FieldRowValue>{ver.version}</FieldRowValue>
                  </FieldRow>
                </FieldCard>
                {ver.updatable ? (
                  <>
                    {upd.phase === 'updated' ? (
                      <Button $variant="update" onClick={() => window.location.reload()}>
                        <Icon.refresh width="18" height="18" />
                        <span>{t('reloadNow')}</span>
                      </Button>
                    ) : (
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
                      </>
                    )}
                    {upd.text && (
                      <FieldHint $error={upd.phase === 'error'}>{upd.text}</FieldHint>
                    )}
                  </>
                ) : (
                  <FieldHint>{t('updatesOnSpeaker')}</FieldHint>
                )}
              </>
            )}
          </Form>
        </SheetBody>
      </SheetEl>
    </>
  );
}

export default SettingsSheet;
