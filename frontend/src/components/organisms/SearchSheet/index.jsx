import { useState, useEffect, useRef } from 'react';
import { Icon } from '../../atoms/Icon';
import { StationRow } from '../../molecules/StationRow';
import {
  SheetScrim, SheetEl, SheetHandle, SheetBody, SheetHeader, Eyebrow,
} from '../../molecules/Sheet';
import { searchTuneIn } from '../../../lib/api';
import { useI18n } from '../../../lib/i18n';
import {
  SheetSearch, SheetClear, SheetRows, SheetEmpty, SheetEmptyQ,
} from './styled';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

export function SearchSheet({ open, mode, speakerName, onClose, onPick }) {
  const { t } = useI18n();
  const [query, setQuery] = useState('');
  const [tuneInResults, setTuneInResults] = useState([]);
  const [searching, setSearching] = useState(false);
  const inputRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    setQuery('');
    setTuneInResults([]);
    const focusT = setTimeout(() => inputRef.current?.focus(), 250);
    return () => clearTimeout(focusT);
  }, [open]);

  // Esc to close
  useEffect(() => {
    if (!open) return;
    const f = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', f);
    return () => window.removeEventListener('keydown', f);
  }, [open, onClose]);

  // Search TuneIn when query changes (debounced). `alive` guards against a slow
  // response landing after the query was cleared or changed, which would otherwise
  // flash stale results or leave the spinner stuck on.
  useEffect(() => {
    const q = query.trim();
    if (!q) { setTuneInResults([]); setSearching(false); return undefined; }
    let alive = true;
    const t = setTimeout(async () => {
      setSearching(true);
      const results = await searchTuneIn(q);
      if (!alive) return;
      setTuneInResults(results);
      setSearching(false);
    }, 400);
    return () => { alive = false; clearTimeout(t); };
  }, [query]);

  const heading = mode?.mode === 'assign' ? t('chooseStation') : t('discoverStations');
  const sub = mode?.mode === 'assign' ? `${t('forSlot')} ${mode.slot ?? 1}` : `${t('forSpeaker')} ${clean(speakerName) || 'SoundTouch'}`;

  // Stations come live from TuneIn — its ids are always current, so a clicked
  // result plays the right station.
  const allResults = query.trim() ? tuneInResults : [];

  return (
    <>
      <SheetScrim $open={open} onClick={onClose} />
      <SheetEl $open={open} role="dialog" aria-modal="true">
        <SheetHandle />
        <SheetHeader onClose={onClose} closeLabel={t('close')} headline={heading}>
          <Eyebrow>{sub}</Eyebrow>
        </SheetHeader>

        <SheetSearch>
          <Icon.search width="18" height="18" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('searchPlaceholder')}
            inputMode="search"
          />
          {query && (
            <SheetClear onClick={() => setQuery('')} aria-label={t('clear')}>
              <Icon.close width="16" height="16" />
            </SheetClear>
          )}
        </SheetSearch>

        <SheetBody>
          {!query.trim() ? (
            <SheetEmpty>
              <div aria-hidden="true">
                <Icon.search width="26" height="26" />
              </div>
              <div>{t('searchPrompt')}</div>
            </SheetEmpty>
          ) : allResults.length === 0 ? (
            <SheetEmpty>
              <SheetEmptyQ>"{query}"</SheetEmptyQ>
              <div>{searching ? t('searching') : t('noStations')}</div>
            </SheetEmpty>
          ) : (
            <SheetRows>
              {allResults.map((s, i) => (
                <StationRow key={s.tuneInId || s.id || i} station={s} onPick={() => onPick(s)} />
              ))}
            </SheetRows>
          )}
        </SheetBody>
      </SheetEl>
    </>
  );
}

export default SearchSheet;
