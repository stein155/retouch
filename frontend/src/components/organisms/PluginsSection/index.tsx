import { useState, useEffect, useCallback, useRef } from 'react';
import { Icon } from '../../atoms/Icon';
import { Spinner } from '../../atoms/Spinner';
import { Button } from '../../atoms/Button';
import { Toggle } from '../../atoms/Toggle';
import { QRCode } from '../../atoms/QRCode';
import {
  FieldHint, FieldCard, FieldRow, FieldRowLabel, FieldRowInput, FieldRowValue, SetEyebrow,
} from '../../molecules/Field';
import { useI18n } from '../../../lib/i18n';
import {
  getPlugins, installPlugin, removePlugin, uploadPlugin,
  getPluginManifest, pluginAction, getPluginLatest,
} from '../../../lib/api';
import {
  QrCardEl, QrQuiet, QrCodeBtn, QrCodeText, QrCopy,
  PlugHead, PlugName, PlugMeta, PlugAction,
} from './styled';
import type {
  PluginsResponse, InstalledPluginInfo, CatalogPluginInfo,
  PluginManifest, ManifestSection, ManifestField, ManifestAction,
} from '../../../lib/types';

// The plugin manifest is server-driven and dynamic; these locally refine the
// shared Manifest* shapes with the concrete extra properties this renderer reads.
type ManifestFieldT = ManifestField & {
  key: string;
  code?: string;
  placeholder?: string;
  value?: string | boolean;
};
type ManifestToggleT = { key: string; label?: string; value?: unknown };
type ManifestRowT = { id: string; label?: string; toggles?: ManifestToggleT[] };
type ManifestActionT = ManifestAction & { confirm?: string; style?: string };
type ManifestSectionT = ManifestSection & { text?: string };
type ManifestStatus = { level?: string; text?: string };

// A plugin shown in the settings subpage carries its resolved (localised) title.
type OpenPlugin = InstalledPluginInfo & { title: string };

// A coloured status dot, matching the MQTT section's convention.
const statusColor = (level: unknown): string => {
  const map: Record<string, string> = {
    ok: '#2ecc71', warn: '#f1c40f', error: '#e74c3c',
  };
  return (typeof level === 'string' && map[level]) || 'var(--muted, #9aa0a6)';
};

// catStr prefers a localised catalog string (pluginCat_<name>_<suffix>) when the
// app ships one, else falls back to the server-provided English value. makeT
// returns the key itself when a translation is missing, which is how we detect it.
function catStr(t: (key: string) => string, name: string, suffix: string, fallback: string): string {
  const key = `pluginCat_${name}_${suffix}`;
  const v = t(key);
  return v === key ? fallback : v;
}

function QrField({ field }: { field: ManifestFieldT }) {
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
      {field.value && <QrQuiet><QRCode value={String(field.value)} size={150} /></QrQuiet>}
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
function PluginPanel({ name }: { name: string }) {
  const { t, lang } = useI18n();
  const [manifest, setManifest] = useState<PluginManifest | null>(null);
  const [values, setValues] = useState<Record<string, string | boolean>>({}); // field.key -> string|bool
  const [rows, setRows] = useState<Record<string, Record<string, boolean>>>({}); // row.id -> { toggleKey: bool }
  const [busy, setBusy] = useState('');      // action id in flight
  const [err, setErr] = useState('');
  const alive = useRef(true);

  useEffect(() => () => { alive.current = false; }, []);

  // Adopt a manifest: replace the schema and seed the editable state from it.
  const adopt = useCallback((m: PluginManifest | null) => {
    if (!m || !alive.current) return;
    setManifest(m);
    const v: Record<string, string | boolean> = {};
    const r: Record<string, Record<string, boolean>> = {};
    for (const sec of m.sections || []) {
      for (const f of (sec.fields || []) as ManifestFieldT[]) v[f.key] = f.value ?? (f.type === 'toggle' ? false : '');
      for (const row of (sec.rows || []) as ManifestRowT[]) {
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
    let timer: ReturnType<typeof setTimeout> | undefined;
    let tries = 0;
    const attempt = () => {
      getPluginManifest(name, lang).then((m) => {
        if (!on) return;
        if (m) { adopt(m); return; }
        if (tries++ < 20) timer = setTimeout(attempt, 1500);
      });
    };
    attempt();
    return () => { on = false; clearTimeout(timer); };
  }, [name, lang, adopt]);

  const runAction = async (action: ManifestActionT) => {
    if (busy) return;
    if (action.confirm && !window.confirm(action.confirm)) return;
    setBusy(action.id);
    setErr('');
    try {
      const next = await pluginAction(name, action.id, { values, rows }, lang);
      adopt(next as PluginManifest | null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      if (alive.current) setBusy('');
    }
  };

  if (!manifest) {
    return <FieldHint>{t('pluginLoading')}…</FieldHint>;
  }

  const st = manifest.status as unknown as ManifestStatus | undefined;
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
      {((manifest.sections || []) as ManifestSectionT[]).map((sec, si) => (
        <div key={si} style={{ marginTop: si === 0 ? 4 : 14 }}>
          {sec.title && <FieldRowLabel as="div" style={{ fontWeight: 600, marginBottom: 6 }}>{sec.title}</FieldRowLabel>}
          {sec.text && <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{sec.text}</FieldHint>}

          {((sec.fields || []) as ManifestFieldT[]).filter((f) => f.type === 'qr').map((f) => (
            <QrField key={f.key} field={f} />
          ))}

          {(((sec.fields || []) as ManifestFieldT[]).some((f) => f.type !== 'qr') || (sec.rows?.length ?? 0) > 0) && (
            <FieldCard>
              {((sec.fields || []) as ManifestFieldT[]).filter((f) => f.type !== 'qr').map((f) => (
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
                      value={(values[f.key] as string) ?? ''}
                      placeholder={f.placeholder || ''}
                      autoComplete={f.type === 'password' ? 'current-password' : 'off'}
                      onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                    />
                  )}
                </FieldRow>
              ))}
              {((sec.rows || []) as ManifestRowT[]).map((row) => (
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

          {((sec.actions || []) as ManifestActionT[]).map((a) => (
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

// One installed plugin, shown as a single tappable row: name + "Geïnstalleerd" on
// the left and an "Instellingen ›" affordance on the right. Tapping it opens the
// plugin's own settings as a subpage (see PluginSettings) — that's where the QR /
// pairing code and the remove button live — so this list stays compact.
function InstalledPlugin({ p, entry, onOpen }: {
  p: InstalledPluginInfo;
  entry?: CatalogPluginInfo;
  onOpen: (p: OpenPlugin) => void;
}) {
  const { t } = useI18n();
  const title = catStr(t, p.name, 'title', entry?.title || p.name);

  return (
    <div style={{ marginTop: 12 }}>
      <FieldCard>
        <PlugHead type="button" onClick={() => onOpen({ ...p, title })}>
          <span style={{ flex: 1, minWidth: 0 }}>
            <PlugName>{title}</PlugName>
            <PlugMeta>{t('pluginInstalled')}</PlugMeta>
          </span>
          <PlugAction>
            <span>{t('pluginSettings')}</span>
            <Icon.chevron width="17" height="17" />
          </PlugAction>
        </PlugHead>
      </FieldCard>
    </div>
  );
}

// PluginSettings is the subpage a plugin opens into: its server-driven config
// panel (status, fields, QR / pairing code, actions), then a maintenance block —
// version, an over-the-air update when a newer release exists, and remove. The
// enclosing sheet supplies the header + back arrow, so this renders body only.
export function PluginSettings({ p, onRemoved }: { p: InstalledPluginInfo; onRemoved: () => void }) {
  const { t } = useI18n();
  const [removing, setRemoving] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [version, setVersion] = useState<string | null>(p.version || '');
  const [latest, setLatest] = useState<string | null>(null); // newest available tag, or null
  const [panelKey, setPanelKey] = useState(0); // bump to re-mount the panel after an update

  // Look up the newest release so we can offer an update. Sideloaded plugins have
  // no release to compare against, so skip the lookup for them.
  useEffect(() => {
    if (p.sideloaded) return undefined;
    let on = true;
    getPluginLatest(p.name).then((r) => { if (on) setLatest(r?.tag || null); });
    return () => { on = false; };
  }, [p.name, p.sideloaded]);

  // A re-install pulls the latest release over the existing one: the binary is
  // replaced and the child restarted, but its state dir (the HomeKit pairing) is
  // kept. Re-mount the panel afterwards so it re-polls the restarted plugin.
  const update = async () => {
    if (updating) return;
    setUpdating(true);
    try {
      await installPlugin(p.name);
      setVersion(latest);
      setPanelKey((k) => k + 1);
    } catch { /* leave the current version in place */ } finally {
      setUpdating(false);
    }
  };

  const remove = async () => {
    if (!window.confirm(t('pluginRemoveConfirm'))) return;
    setRemoving(true);
    try { await removePlugin(p.name); onRemoved(); } catch { setRemoving(false); }
  };

  const updatable = !p.sideloaded && latest && latest !== version;

  return (
    <>
      {p.lastErr && !p.running && <FieldHint style={{ marginTop: 0 }} $error>{p.lastErr}</FieldHint>}
      <PluginPanel key={panelKey} name={p.name} />

      {version && (
        <FieldCard style={{ marginTop: 12 }}>
          <FieldRow>
            <FieldRowLabel as="span">{t('version')}</FieldRowLabel>
            <FieldRowValue>{version}</FieldRowValue>
          </FieldRow>
        </FieldCard>
      )}
      {p.sideloaded && <FieldHint>{t('pluginUpdatable')}</FieldHint>}
      {updatable && (
        <Button $variant="update" onClick={update} disabled={updating}>
          {updating ? <Spinner $scan /> : <Icon.download width="18" height="18" />}
          <span>{updating ? t('pluginUpdating') : `${t('pluginUpdate')} ${latest}`}</span>
        </Button>
      )}
      {!p.sideloaded && latest && latest === version && <FieldHint>{t('upToDate')}</FieldHint>}

      <Button $variant="delete" onClick={remove} disabled={removing}>
        {removing ? <Spinner $scan /> : <Icon.trash width="17" height="17" />}
        <span>{t('pluginRemove')}</span>
      </Button>
    </>
  );
}

// A catalog entry that isn't installed yet: install from its release, or sideload a
// locally-built binary (for a plugin whose release repo is still private).
function CatalogPlugin({ entry, onChanged, sideloadAllowed }: {
  entry: CatalogPluginInfo;
  onChanged: () => void;
  sideloadAllowed: boolean;
}) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const fileRef = useRef<HTMLInputElement | null>(null);

  const install = async () => {
    setBusy(true); setErr('');
    try { await installPlugin(entry.name); onChanged(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };

  const sideload = async (file: File | undefined) => {
    if (!file) return;
    setBusy(true); setErr('');
    try { await uploadPlugin(entry.name, file); onChanged(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };

  // Catalog entries carry English title/description from the server; prefer a
  // localised string when the app ships one for this plugin.
  const title = catStr(t, entry.name, 'title', entry.title || entry.name);
  const description = catStr(t, entry.name, 'desc', entry.description);

  return (
    <div style={{ marginTop: 12 }}>
      <FieldCard style={{ padding: '14px 16px' }}>
        <div style={{ fontSize: 14.5, fontWeight: 600, color: 'var(--ink-2)' }}>{title}</div>
        {description && (
          <div style={{ fontSize: 12.5, lineHeight: 1.4, marginTop: 4, color: 'var(--ink-3)' }}>{description}</div>
        )}
        <Button $variant="update" onClick={install} disabled={busy}>
          {busy ? <Spinner $scan /> : <Icon.download width="18" height="18" />}
          <span>{busy ? t('pluginInstalling') : t('pluginInstall')}</span>
        </Button>
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
export function PluginsSection({ open, onOpen }: {
  open: boolean;
  onOpen: (p: OpenPlugin) => void;
}) {
  const { t } = useI18n();
  const [data, setData] = useState<PluginsResponse | null>(null); // { installed, catalog } | null

  const refresh = useCallback(() => { getPlugins().then(setData); }, []);
  useEffect(() => { if (open) refresh(); }, [open, refresh]);

  const installed = data?.installed || [];
  const installedNames = new Set(installed.map((p) => p.name));
  const catalog = data?.catalog || [];
  const catalogByName = new Map(catalog.map((e) => [e.name, e] as const));
  const available = catalog.filter((e) => !installedNames.has(e.name));
  const onSpeaker = !!data; // /api/plugins answered

  return (
    <>
      <FieldHint style={{ marginTop: 0, marginBottom: 8 }}>{t('pluginsHint')}</FieldHint>
      {!onSpeaker && <FieldHint>{t('pluginsOnSpeaker')}</FieldHint>}
      {installed.map((p) => (
        <InstalledPlugin key={p.name} p={p} entry={catalogByName.get(p.name)} onOpen={onOpen} />
      ))}
      {available.map((e) => (
        <CatalogPlugin key={e.name} entry={e} onChanged={refresh} sideloadAllowed={!!(data as (PluginsResponse & { sideload?: boolean }) | null)?.sideload} />
      ))}
    </>
  );
}

export default PluginsSection;
