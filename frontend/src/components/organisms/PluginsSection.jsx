import { useState, useEffect, useCallback, useRef } from 'react';
import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { Spinner } from '../atoms/Spinner';
import { Button } from '../atoms/Button';
import { Toggle } from '../atoms/Toggle';
import { QRCode } from '../atoms/QRCode';
import {
  FieldHint, FieldCard, FieldRow, FieldRowLabel, FieldRowInput, FieldRowValue, SetEyebrow,
} from '../molecules/Field';
import { useI18n } from '../../lib/i18n';
import {
  getPlugins, installPlugin, removePlugin, uploadPlugin,
  getPluginManifest, pluginAction,
} from '../../lib/api';

// A coloured status dot, matching the MQTT section's convention.
const statusColor = (level) => ({
  ok: '#2ecc71', warn: '#f1c40f', error: '#e74c3c',
}[level] || 'var(--muted, #9aa0a6)');

// --- qr field: a scannable pairing code plus its typeable digits -----------
// Used by plugins like Apple Home. The field carries { value } (the payload the QR
// encodes) and an optional { code } (the human-readable version, tap to copy).
const QrCardEl = styled(FieldCard)`
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 18px 16px;
`;

const QrQuiet = styled.div`
  padding: 12px;
  background: #fff;
  border-radius: 14px;
  box-shadow: 0 0 0 1px rgba(31, 24, 20, 0.06);
  line-height: 0;
`;

const QrCodeBtn = styled.button`
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 18px;
  padding: 8px 10px 8px 16px;
  background: var(--surface-2);
  border-radius: 12px;
  -webkit-tap-highlight-color: transparent;
`;

const QrCodeText = styled.span`
  font-size: 21px;
  font-weight: 700;
  letter-spacing: 0.1em;
  font-variant-numeric: tabular-nums;
  color: var(--ink);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
`;

const QrCopy = styled.span`
  display: flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
  padding: 6px 10px;
  border-radius: 9px;
  background: #fff;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--accent);
`;

function QrField({ field }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    if (!field.code) return;
    try {
      await navigator.clipboard.writeText(field.code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch { /* clipboard unavailable */ }
  };
  return (
    <QrCardEl>
      {field.label && <SetEyebrow style={{ marginBottom: 14 }}>{field.label}</SetEyebrow>}
      {field.value && <QrQuiet><QRCode value={field.value} size={150} /></QrQuiet>}
      {field.code && (
        <QrCodeBtn type="button" onClick={copy} aria-label={t('copy')}>
          <QrCodeText>{field.code}</QrCodeText>
          <QrCopy>
            {copied && <Icon.check width="15" height="15" />}
            {copied ? t('copied') : t('copy')}
          </QrCopy>
        </QrCodeBtn>
      )}
    </QrCardEl>
  );
}

// PluginPanel renders one installed plugin's server-driven settings UI. The plugin
// serves a "manifest" (status + sections of fields/rows/actions); every action POSTs
// the current inputs back and returns the NEW manifest, which we re-render. Multi-step
// flows (log in → 2FA code → logged in) need no special-casing here: each step is just
// the next manifest the plugin sends.
function PluginPanel({ name }) {
  const { t } = useI18n();
  const [manifest, setManifest] = useState(null);
  const [values, setValues] = useState({}); // field.key -> string|bool
  const [rows, setRows] = useState({});      // row.id -> { toggleKey: bool }
  const [busy, setBusy] = useState('');      // action id in flight
  const [err, setErr] = useState('');
  const alive = useRef(true);

  useEffect(() => () => { alive.current = false; }, []);

  // Adopt a manifest: replace the schema and seed the editable state from it.
  const adopt = useCallback((m) => {
    if (!m || !alive.current) return;
    setManifest(m);
    const v = {};
    const r = {};
    for (const sec of m.sections || []) {
      for (const f of sec.fields || []) v[f.key] = f.value ?? (f.type === 'toggle' ? false : '');
      for (const row of sec.rows || []) {
        r[row.id] = {};
        for (const tg of row.toggles || []) r[row.id][tg.key] = !!tg.value;
      }
    }
    setValues(v);
    setRows(r);
  }, []);

  // Fetch the manifest, retrying while the plugin is still booting (right after an
  // install the child needs a moment before its HTTP server answers).
  useEffect(() => {
    let on = true;
    let timer;
    let tries = 0;
    const attempt = () => {
      getPluginManifest(name).then((m) => {
        if (!on) return;
        if (m) { adopt(m); return; }
        if (tries++ < 20) timer = setTimeout(attempt, 1500);
      });
    };
    attempt();
    return () => { on = false; clearTimeout(timer); };
  }, [name, adopt]);

  const runAction = async (action) => {
    if (busy) return;
    if (action.confirm && !window.confirm(action.confirm)) return;
    setBusy(action.id);
    setErr('');
    try {
      const next = await pluginAction(name, action.id, { values, rows });
      adopt(next);
    } catch (e) {
      setErr(e.message || String(e));
    } finally {
      if (alive.current) setBusy('');
    }
  };

  if (!manifest) {
    return <FieldHint>{t('pluginNotRunning')}…</FieldHint>;
  }

  const st = manifest.status;
  return (
    <>
      {st && (
        <FieldHint style={{ marginTop: 0 }}>
          <span style={{
            display: 'inline-block', width: 8, height: 8, borderRadius: 99,
            marginRight: 6, verticalAlign: 'middle', background: statusColor(st.level),
          }} />
          {st.text}
        </FieldHint>
      )}
      {(manifest.sections || []).map((sec, si) => (
        <div key={si} style={{ marginTop: si === 0 ? 4 : 14 }}>
          {sec.title && <FieldRowLabel as="div" style={{ fontWeight: 600, marginBottom: 6 }}>{sec.title}</FieldRowLabel>}
          {sec.text && <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{sec.text}</FieldHint>}

          {(sec.fields || []).filter((f) => f.type === 'qr').map((f) => (
            <QrField key={f.key} field={f} />
          ))}

          {((sec.fields || []).some((f) => f.type !== 'qr') || sec.rows?.length > 0) && (
            <FieldCard>
              {(sec.fields || []).filter((f) => f.type !== 'qr').map((f) => (
                <FieldRow key={f.key}>
                  <FieldRowLabel htmlFor={`pl-${name}-${f.key}`}>{f.label}</FieldRowLabel>
                  {f.type === 'toggle' ? (
                    <Toggle
                      on={!!values[f.key]}
                      onClick={() => setValues((v) => ({ ...v, [f.key]: !v[f.key] }))}
                      aria-label={f.label}
                      style={{ marginLeft: 'auto' }}
                    />
                  ) : (
                    <FieldRowInput
                      id={`pl-${name}-${f.key}`}
                      type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'}
                      inputMode={f.type === 'otp' ? 'numeric' : undefined}
                      value={values[f.key] ?? ''}
                      placeholder={f.placeholder || ''}
                      autoComplete={f.type === 'password' ? 'current-password' : 'off'}
                      onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                    />
                  )}
                </FieldRow>
              ))}
              {(sec.rows || []).map((row) => (
                <FieldRow key={row.id}>
                  <FieldRowLabel as="span">{row.label}</FieldRowLabel>
                  <span style={{ marginLeft: 'auto', display: 'flex', gap: 14, alignItems: 'center' }}>
                    {(row.toggles || []).map((tg) => (
                      <span key={tg.key} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        <FieldRowValue as="span" style={{ fontSize: 12 }}>{tg.label}</FieldRowValue>
                        <Toggle
                          on={!!rows[row.id]?.[tg.key]}
                          onClick={() => setRows((r) => ({ ...r, [row.id]: { ...r[row.id], [tg.key]: !r[row.id]?.[tg.key] } }))}
                          aria-label={`${row.label} ${tg.label}`}
                        />
                      </span>
                    ))}
                  </span>
                </FieldRow>
              ))}
            </FieldCard>
          )}

          {(sec.actions || []).map((a) => (
            <Button
              key={a.id}
              $variant="update"
              onClick={() => runAction(a)}
              disabled={!!busy}
              style={{ marginTop: 8, ...(a.style === 'danger' ? { background: 'var(--ink)' } : {}) }}
            >
              {busy === a.id ? <Spinner $scan /> : <Icon.check width="18" height="18" />}
              <span>{a.label}</span>
            </Button>
          ))}
        </div>
      ))}
      {err && <FieldHint $error>{err}</FieldHint>}
    </>
  );
}

// One installed plugin: a header row (name, run status, remove) plus its config panel.
function InstalledPlugin({ p, onRemove }) {
  const { t } = useI18n();
  const [removing, setRemoving] = useState(false);

  const remove = async () => {
    if (!window.confirm(t('pluginRemoveConfirm'))) return;
    setRemoving(true);
    try { await removePlugin(p.name); onRemove(); } catch { setRemoving(false); }
  };

  return (
    <div style={{ marginTop: 12 }}>
      <FieldCard>
        <FieldRow>
          <FieldRowLabel as="span" style={{ textTransform: 'capitalize' }}>{p.name}</FieldRowLabel>
          <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
            <FieldRowValue as="span" style={{ fontSize: 12, color: p.running ? '#2ecc71' : 'var(--muted, #9aa0a6)' }}>
              {p.running ? t('pluginRunning') : t('pluginNotRunning')}
            </FieldRowValue>
            <Button $variant="ghost" onClick={remove} disabled={removing} aria-label={t('pluginRemove')} style={{ padding: '4px 10px' }}>
              {removing ? <Spinner $scan /> : <span>{t('pluginRemove')}</span>}
            </Button>
          </span>
        </FieldRow>
      </FieldCard>
      {p.sideloaded && <FieldHint>{t('pluginUpdatable')}</FieldHint>}
      {p.lastErr && !p.running && <FieldHint $error>{p.lastErr}</FieldHint>}
      <PluginPanel name={p.name} />
    </div>
  );
}

// A catalog entry that isn't installed yet: install from its release, or sideload a
// locally-built binary (for a plugin whose release repo is still private).
function CatalogPlugin({ entry, onChanged, sideloadAllowed }) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const fileRef = useRef(null);

  const install = async () => {
    setBusy(true); setErr('');
    try { await installPlugin(entry.name); onChanged(); } catch (e) { setErr(e.message || String(e)); setBusy(false); }
  };

  const sideload = async (file) => {
    if (!file) return;
    setBusy(true); setErr('');
    try { await uploadPlugin(entry.name, file); onChanged(); } catch (e) { setErr(e.message || String(e)); setBusy(false); }
  };

  // Catalog entries carry English title/description from the server. Prefer a
  // localised string when the app ships one for this plugin (key present), else
  // fall back to the server value. makeT returns the key itself when missing.
  const local = (suffix, fallback) => {
    const key = `pluginCat_${entry.name}_${suffix}`;
    const v = t(key);
    return v === key ? fallback : v;
  };
  const title = local('title', entry.title || entry.name);
  const description = local('desc', entry.description);

  return (
    <div style={{ marginTop: 12 }}>
      <FieldCard>
        <FieldRow>
          <span style={{ flex: 1, minWidth: 0 }}>
            <FieldRowLabel as="div">{title}</FieldRowLabel>
            {description && <FieldRowValue as="div" style={{ flex: 'none', fontSize: 12, marginTop: 2, textAlign: 'left', fontWeight: 400, color: 'var(--ink-3)' }}>{description}</FieldRowValue>}
          </span>
          <Button $variant="update" onClick={install} disabled={busy} style={{ flexShrink: 0 }}>
            {busy ? <Spinner $scan /> : <Icon.download width="18" height="18" />}
            <span>{busy ? t('pluginInstalling') : t('pluginInstall')}</span>
          </Button>
        </FieldRow>
      </FieldCard>
      {sideloadAllowed && (
        <>
          <input
            ref={fileRef}
            type="file"
            style={{ display: 'none' }}
            onChange={(e) => sideload(e.target.files?.[0])}
          />
          <Button $variant="ghost" onClick={() => fileRef.current?.click()} disabled={busy} style={{ marginTop: 6 }}>
            <Icon.plus width="18" height="18" />
            <span>{t('pluginSideload')}</span>
          </Button>
          <FieldHint style={{ marginTop: 6 }}>{t('pluginSideloadHint')}</FieldHint>
        </>
      )}
      {err && <FieldHint $error>{err}</FieldHint>}
    </div>
  );
}

// PluginsSection lists installed plugins (each with its config panel) and the catalog
// of installable ones. Off-speaker the API returns nothing, so the section shows a
// hint instead. Refetches on open and after every install/remove.
export function PluginsSection({ open }) {
  const { t } = useI18n();
  const [data, setData] = useState(null); // { installed, catalog } | null

  const refresh = useCallback(() => { getPlugins().then(setData); }, []);
  useEffect(() => { if (open) refresh(); }, [open, refresh]);

  const installed = data?.installed || [];
  const installedNames = new Set(installed.map((p) => p.name));
  const available = (data?.catalog || []).filter((e) => !installedNames.has(e.name));
  const onSpeaker = !!data; // /api/plugins answered

  return (
    <>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('pluginsHint')}</FieldHint>
      {!onSpeaker && <FieldHint>{t('pluginsOnSpeaker')}</FieldHint>}
      {installed.map((p) => (
        <InstalledPlugin key={p.name} p={p} onRemove={refresh} />
      ))}
      {available.map((e) => (
        <CatalogPlugin key={e.name} entry={e} onChanged={refresh} sideloadAllowed={!!data?.sideload} />
      ))}
    </>
  );
}

export default PluginsSection;
