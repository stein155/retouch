import { useState, useEffect, useRef } from 'react';
import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { StationRow } from '../molecules/StationRow';
import {
  SheetScrim, SheetEl, SheetHandle, SheetBody, SheetHeader, Eyebrow,
} from '../molecules/Sheet';
import { searchTuneIn } from '../../lib/api';
import { useI18n } from '../../lib/i18n';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

const SheetSearch = styled.div`
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 20px 10px;
  padding: 14px 16px;
  background: #fff;
  border-radius: 16px;
  color: var(--ink-2);
  box-shadow: var(--shadow-sm);
  transition: box-shadow 200ms ease;

  &:focus-within { box-shadow: var(--shadow); }

  input {
    flex: 1;
    background: transparent;
    border: 0;
    outline: none;
    color: var(--ink);
    font-size: 15px;
    font-weight: 500;
  }
  input::placeholder { color: var(--ink-3); font-weight: 400; }
`;

const SheetClear = styled.button`
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  color: var(--ink-2);
  background: var(--surface-3);

  &:hover { background: var(--ink); color: #fff; }
`;

const SheetRows = styled.div`
  display: flex;
  flex-direction: column;
`;

const SheetEmpty = styled.div`
  padding: 60px 20px;
  text-align: center;
  color: var(--ink-2);
  font-size: 13px;
  font-weight: 500;
`;

const SheetEmptyQ = styled.div`
  color: var(--ink);
  font-size: 18px;
  font-weight: 700;
  margin-bottom: 8px;
`;

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
