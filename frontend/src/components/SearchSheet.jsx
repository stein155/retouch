import { useState, useEffect, useRef } from 'react';
import { StationLogo } from './StationLogo';
import { Icon } from './Icons';
import { searchTuneIn } from '../lib/api';
import { useI18n } from '../lib/i18n';

const cx = (...a) => a.filter(Boolean).join(' ');

function StationRow({ station, onPick }) {
  const id = station.id || null;
  return (
    <button className="row" onClick={onPick}>
      <div className="row-art">
        <StationLogo id={id} name={station.name} tuneInId={station.tuneInId} logo={station.logo} />
      </div>
      <div className="row-text">
        <div className="row-name">{station.name}</div>
        <div className="row-sub">
          <span>{station.genre}</span>
          {station.tagline && <><span className="row-dot">·</span><span className="row-tagline">{station.tagline}</span></>}
          {station.country && <><span className="row-dot">·</span><span>{station.country}</span></>}
        </div>
      </div>
      <div className="row-play"><Icon.play width="12" height="12" /></div>
    </button>
  );
}

export function SearchSheet({ open, mode, speakerName, onClose, onPick }) {
  const { t } = useI18n();
  const [query, setQuery] = useState('');
  const [tuneInResults, setTuneInResults] = useState([]);
  const [searching, setSearching] = useState(false);
  const inputRef = useRef(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setTuneInResults([]);
      setTimeout(() => inputRef.current?.focus(), 250);
    }
  }, [open]);

  // Esc to close
  useEffect(() => {
    if (!open) return;
    const f = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', f);
    return () => window.removeEventListener('keydown', f);
  }, [open, onClose]);

  // Search TuneIn when query changes (debounced)
  useEffect(() => {
    const q = query.trim();
    if (!q) { setTuneInResults([]); return; }
    const t = setTimeout(async () => {
      setSearching(true);
      const results = await searchTuneIn(q);
      setTuneInResults(results);
      setSearching(false);
    }, 400);
    return () => clearTimeout(t);
  }, [query]);

  const heading = mode?.mode === 'assign' ? t('chooseStation') : t('discoverStations');
  const sub = mode?.mode === 'assign' ? `${t('forSlot')} ${mode.slot ?? 1}` : `${t('forSpeaker')} ${speakerName}`;

  // Stations come live from TuneIn — its ids are always current, so a clicked
  // result plays the right station.
  const allResults = query.trim() ? tuneInResults : [];

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
            <div className="eyebrow">{sub}</div>
            <div className="sheet-headline">{heading}</div>
          </div>
        </header>

        <div className="sheet-search">
          <Icon.search width="18" height="18" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('searchPlaceholder')}
            inputMode="search"
          />
          {query && (
            <button className="sheet-clear" onClick={() => setQuery('')} aria-label={t('clear')}>
              <Icon.close width="16" height="16" />
            </button>
          )}
        </div>

        <div className="sheet-body">
          {!query.trim() ? (
            <div className="sheet-empty">
              <div className="sheet-empty-icon" aria-hidden="true">
                <Icon.search width="26" height="26" />
              </div>
              <div>{t('searchPrompt')}</div>
            </div>
          ) : allResults.length === 0 ? (
            <div className="sheet-empty">
              <div className="sheet-empty-q">"{query}"</div>
              <div>{searching ? t('searching') : t('noStations')}</div>
            </div>
          ) : (
            <div className="sheet-rows">
              {allResults.map((s, i) => (
                <StationRow key={s.tuneInId || s.id || i} station={s} onPick={() => onPick(s)} />
              ))}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
