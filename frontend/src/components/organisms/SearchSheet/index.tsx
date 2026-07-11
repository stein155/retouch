import { useState, useEffect, useRef } from 'react';
import { Icon } from '../../atoms/Icon';
import { StationRow } from '../../molecules/StationRow';
import { Spinner } from '../../atoms/Spinner';
import {
  SheetScrim, SheetEl, SheetHandle, SheetBody, SheetHeader, Eyebrow,
} from '../../molecules/Sheet';
import { searchTuneIn, browseTuneIn } from '../../../lib/api';
import { useI18n, tuneInLocale } from '../../../lib/i18n';
import {
  SheetSearch, SheetClear, SheetRows, SheetEmpty, SheetEmptyQ,
  BrowseBar, BrowseBack, CatRow,
} from './styled';
import type { Station, BrowseCategory, BrowseResult } from '../../../lib/types';

const clean = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

interface Props {
  open: boolean;
  mode: { mode: string; slot?: number } | null;
  speakerName: string;
  onClose: () => void;
  onPick: (station: Station) => void;
}

const EMPTY: BrowseResult = { categories: [], stations: [] };

export function SearchSheet({ open, mode, speakerName, onClose, onPick }: Props) {
  const { t, lang } = useI18n();
  const [query, setQuery] = useState('');
  const [tuneInResults, setTuneInResults] = useState<Station[]>([]);
  const [searching, setSearching] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Browse (shown while the search box is empty): a stack of drilled-into
  // categories + the current level's contents. `browseGen` discards a slow
  // response that lands after the user navigated elsewhere.
  const [stack, setStack] = useState<BrowseCategory[]>([]);
  const [level, setLevel] = useState<BrowseResult>(EMPTY);
  const [browsing, setBrowsing] = useState(false);
  const browseGen = useRef(0);

  const loadBrowse = (path: string | undefined, nextStack: BrowseCategory[]) => {
    const gen = ++browseGen.current;
    setBrowsing(true);
    setStack(nextStack);
    browseTuneIn(path, tuneInLocale(lang)).then((res) => {
      if (gen !== browseGen.current) return; // superseded by a newer navigation
      setLevel(res);
      setBrowsing(false);
    });
  };

  useEffect(() => {
    if (!open) return undefined;
    setQuery('');
    setTuneInResults([]);
    loadBrowse(undefined, []); // root categories
    const focusT = setTimeout(() => inputRef.current?.focus(), 250);
    return () => clearTimeout(focusT);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Esc to close
  useEffect(() => {
    if (!open) return;
    const f = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', f);
    return () => window.removeEventListener('keydown', f);
  }, [open, onClose]);

  // Search TuneIn when query changes (debounced). `alive` guards against a slow
  // response landing after the query was cleared or changed.
  useEffect(() => {
    const q = query.trim();
    if (!q) { setTuneInResults([]); setSearching(false); return undefined; }
    // Enter the searching state immediately (not inside the debounce) so the body
    // shows "Searching…" during the debounce window instead of flashing
    // "No stations found." before the request is even sent.
    setSearching(true);
    let alive = true;
    const timer = setTimeout(async () => {
      const results = await searchTuneIn(q);
      if (!alive) return;
      setTuneInResults(results);
      setSearching(false);
    }, 400);
    return () => { alive = false; clearTimeout(timer); };
  }, [query]);

  const openCategory = (cat: BrowseCategory) => loadBrowse(cat.path, [...stack, cat]);
  const goBack = () => {
    const next = stack.slice(0, -1);
    loadBrowse(next.length ? next[next.length - 1].path : undefined, next);
  };

  const heading = mode?.mode === 'assign' ? t('chooseStation') : t('discoverStations');
  const sub = mode?.mode === 'assign' ? `${t('forSlot')} ${mode.slot ?? 1}` : `${t('forSpeaker')} ${clean(speakerName) || 'SoundTouch'}`;
  const browseMode = !query.trim();

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

        {browseMode && stack.length > 0 && (
          <BrowseBar>
            <BrowseBack onClick={goBack}>
              <Icon.back width="16" height="16" />
              <span>{stack.length > 1 ? stack[stack.length - 2].title : t('discoverStations')}</span>
            </BrowseBack>
          </BrowseBar>
        )}

        <SheetBody>
          {!browseMode ? (
            tuneInResults.length === 0 ? (
              <SheetEmpty>
                <SheetEmptyQ>"{query}"</SheetEmptyQ>
                <div>{searching ? t('searching') : t('noStations')}</div>
              </SheetEmpty>
            ) : (
              <SheetRows>
                {tuneInResults.map((s, i) => (
                  <StationRow key={s.tuneInId || i} station={s} onPick={() => onPick(s)} />
                ))}
              </SheetRows>
            )
          ) : browsing ? (
            <SheetEmpty><Spinner $scan /></SheetEmpty>
          ) : level.categories.length === 0 && level.stations.length === 0 ? (
            <SheetEmpty><div>{t('noStations')}</div></SheetEmpty>
          ) : (
            <SheetRows>
              {level.categories.map((c) => (
                <CatRow key={c.path} onClick={() => openCategory(c)}>
                  <span>{c.title}</span>
                  <span aria-hidden="true">›</span>
                </CatRow>
              ))}
              {level.stations.map((s, i) => (
                <StationRow key={s.tuneInId || i} station={s} onPick={() => onPick(s)} />
              ))}
            </SheetRows>
          )}
        </SheetBody>
      </SheetEl>
    </>
  );
}

export default SearchSheet;
