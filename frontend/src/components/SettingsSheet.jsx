import { useState, useEffect, useRef, useCallback } from 'react';
import { Icon } from './Icons';
import { useI18n, LANGS } from '../lib/i18n';
import {
  getSettings, saveSettings, getVersion, getReleases, startUpdate,
  findSpeakers, groupSpeaker, ungroupSpeaker,
} from '../lib/api';

const cx = (...a) => a.filter(Boolean).join(' ');
const fmtBass = (v) => (v > 0 ? '+' + v : String(v));

// Centre-origin bass slider over the speaker's real capability range (e.g. -9..0).
// The "origin" tick + fill anchor at the default (or 0 if in range).
function BassSlider({ value, min, max, origin, onChange }) {
  const ref = useRef(null);
  const [dragging, setDragging] = useState(false);
  const span = max - min || 1;

  const update = (clientX) => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (clientX - r.left) / r.width));
    onChange(Math.round(min + ratio * span));
  };

  useEffect(() => {
    if (!dragging) return;
    const move = (e) => update(e.touches ? e.touches[0].clientX : e.clientX);
    const up = () => setDragging(false);
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    window.addEventListener('touchmove', move, { passive: false });
    window.addEventListener('touchend', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      window.removeEventListener('touchmove', move);
      window.removeEventListener('touchend', up);
    };
  }, [dragging]);

  const pct = ((value - min) / span) * 100;
  const originPct = Math.max(0, Math.min(100, ((origin - min) / span) * 100));
  const fillLeft = Math.min(originPct, pct);
  const fillWidth = Math.abs(pct - originPct);

  return (
    <div
      ref={ref}
      className="slider bass-slider"
      role="slider"
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      tabIndex={0}
      onMouseDown={(e) => { setDragging(true); update(e.clientX); }}
      onTouchStart={(e) => { setDragging(true); update(e.touches[0].clientX); }}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { e.preventDefault(); onChange(Math.max(min, value - 1)); }
        if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { e.preventDefault(); onChange(Math.min(max, value + 1)); }
      }}
    >
      <div className="slider-track" />
      <div className="bass-center" style={{ left: `${originPct}%` }} />
      <div className="slider-fill" style={{ left: `${fillLeft}%`, width: `${fillWidth}%` }} />
      <div className="slider-thumb" style={{ left: `${pct}%` }} />
    </div>
  );
}

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
      <div className="form-section" style={{ marginTop: 22 }}>{t('multiroom')}</div>
      <div className="field-hint" style={{ marginTop: 0, marginBottom: 8 }}>{t('multiroomHint')}</div>
      {speakers && speakers.length > 0 && (
        <div className="field-card">
          {speakers.map((sp) => (
            <div className="field-row spk-row" key={sp.deviceId || sp.ip}>
              <span className="spk-icon"><Icon.speaker width="20" height="20" /></span>
              <span className="spk-text">
                <span className="spk-name">{sp.name || 'SoundTouch'}</span>
                {sp.model && <span className="spk-model">{sp.model}</span>}
              </span>
              <button
                type="button"
                className={cx('spk-toggle', sp.grouped && 'is-on')}
                role="switch"
                aria-checked={sp.grouped}
                aria-label={sp.name || sp.ip}
                disabled={!!busy[sp.ip]}
                onClick={() => toggle(sp)}
              >
                <span className="spk-knob" />
              </button>
            </div>
          ))}
        </div>
      )}
      {speakers && speakers.length === 0 && !scanning && (
        <div className="field-hint">{t('noSpeakers')}</div>
      )}
      <button className="update-btn spk-scan" onClick={scan} disabled={scanning}>
        {scanning ? <span className="mp-spinner" /> : <Icon.search width="18" height="18" />}
        <span>{t('findSpeakers')}</span>
      </button>
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
  const [betas, setBetas] = useState([]);                // open-PR beta builds
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
      <div className={cx('sheet-scrim', open && 'is-open')} onClick={onClose} />
      <div className={cx('sheet', open && 'is-open')} role="dialog" aria-modal="true">
        <div className="sheet-handle" />
        <header className="sheet-hdr">
          <button className="sheet-back" onClick={onClose} aria-label={t('close')}>
            <Icon.back width="22" height="22" />
          </button>
          <div className="sheet-title">
            <div className="set-eyebrow">{t('thisRadio')}</div>
            <div className="sheet-headline">{t('settings')}</div>
          </div>
        </header>
        <div className="sheet-body">
          <div className="form">
            <div className="form-section">{t('name')}</div>
            <div className="field-card">
              <div className="field-row">
                <label className="field-row-label" htmlFor="set-name">{t('name')}</label>
                <input
                  id="set-name"
                  className="field-row-input"
                  type="text"
                  value={name}
                  onChange={(e) => onNameInput(e.target.value)}
                  placeholder={t('namePlaceholder')}
                  maxLength={28}
                  autoComplete="off"
                />
              </div>
            </div>
            {host && (
              <div className="field-hint">{t('reachableAt')} <b>{host}</b></div>
            )}

            <div className="form-section" style={{ marginTop: 22 }}>{t('sound')}</div>
            <div className="bass-card">
              <div className="bass-head">
                <span className="bass-name">{t('bass')}</span>
                <span className={cx('bass-val', bass !== caps.default && 'is-set')}>{fmtBass(bass)}</span>
              </div>
              <BassSlider value={bass} min={caps.min} max={caps.max} origin={caps.default} onChange={onBass} />
              <div className="bass-scale">
                <span>{fmtBass(caps.min)}</span>
                <span>{fmtBass(caps.max)}</span>
              </div>
            </div>
            <div className="field-hint">{t('bassHint')}</div>

            <div className="form-section" style={{ marginTop: 22 }}>{t('language')}</div>
            <div className="select-wrap">
              <select
                className="field select"
                value={lang}
                onChange={(e) => onSetLang(e.target.value)}
                aria-label={t('language')}
              >
                {LANGS.map((l) => <option key={l.code} value={l.code}>{l.label}</option>)}
              </select>
              <span className="select-chev" aria-hidden="true"><Icon.chevron width="18" height="18" /></span>
            </div>

            <MultiroomSection />

            {ver && (
              <>
                <div className="form-section" style={{ marginTop: 22 }}>{t('software')}</div>
                <div className="field-card">
                  <div className="field-row">
                    <span className="field-row-label">{t('version')}</span>
                    <span className="field-row-value">{ver.version}</span>
                  </div>
                </div>
                {ver.updatable ? (
                  <>
                    {upd.phase === 'updated' ? (
                      <button className="update-btn" onClick={() => window.location.reload()}>
                        <Icon.refresh width="18" height="18" />
                        <span>{t('reloadNow')}</span>
                      </button>
                    ) : (
                      <>
                        {betas.length > 0 && (
                          <div className="select-wrap" style={{ marginBottom: 8 }}>
                            <select
                              className="field select"
                              value={selTag}
                              onChange={(e) => setSelTag(e.target.value)}
                              disabled={upd.phase === 'busy'}
                              aria-label={t('chooseVersion')}
                            >
                              <option value="">{t('latestStable')}</option>
                              <optgroup label={t('betaBuilds')}>
                                {betas.map((b) => (
                                  <option key={b.tag} value={b.tag}>{b.name}</option>
                                ))}
                              </optgroup>
                            </select>
                            <span className="select-chev" aria-hidden="true"><Icon.chevron width="18" height="18" /></span>
                          </div>
                        )}
                        <button className="update-btn" onClick={onUpdate} disabled={upd.phase === 'busy'}>
                          <Icon.download width="18" height="18" />
                          <span>{selTag ? t('installSelected') : t('updateNow')}</span>
                        </button>
                      </>
                    )}
                    {upd.text && (
                      <div className={cx('field-hint', upd.phase === 'error' && 'is-error')}>{upd.text}</div>
                    )}
                  </>
                ) : (
                  <div className="field-hint">{t('updatesOnSpeaker')}</div>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
