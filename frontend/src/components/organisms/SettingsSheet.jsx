import { useState, useEffect, useRef, useCallback } from 'react';
import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { Spinner } from '../atoms/Spinner';
import { Button } from '../atoms/Button';
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
  getSettings, saveSettings, getVersion, startUpdate,
  findSpeakers, groupSpeaker, ungroupSpeaker,
} from '../../lib/api';

const fmtBass = (v) => (v > 0 ? '+' + v : String(v));

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

// Settings sheet: speaker name + bass (native, via the box) and UI language
// (persisted in STLocal). Live-applies each field. Wifi is intentionally omitted.
export function SettingsSheet({ open, onClose, lang, onSetLang, onNameChange }) {
  const { t } = useI18n();
  const [name, setName] = useState('');
  const [bass, setBass] = useState(0);
  const [caps, setCaps] = useState({ min: -9, max: 0, default: 0 });
  const [host, setHost] = useState('');                  // friendly .local address
  const [ver, setVer] = useState(null);                  // { version, updatable }
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
    });
    getVersion().then((v) => v && setVer(v));
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
      if (v?.version && (target ? v.version === target : v.version !== startV)) {
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
    try { res = await startUpdate(); } catch { setUpd({ phase: 'error', text: t('updateError') }); return; }
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
                      <Button $variant="update" onClick={onUpdate} disabled={upd.phase === 'busy'}>
                        <Icon.download width="18" height="18" />
                        <span>{t('updateNow')}</span>
                      </Button>
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
