import { useState, useRef, useEffect } from 'react';
import styled, { css } from 'styled-components';
import { StationLogo } from '../../atoms/StationLogo';
import { Icon } from '../../atoms/Icon';
import { EqualizerBars } from '../../atoms/EqualizerBars';
import { Spinner } from '../../atoms/Spinner';
import { Skeleton } from '../../atoms/Skeleton';
import { useI18n } from '../../../lib/i18n';
import { pop } from '../../../theme/keyframes';

const clean = (value) => (typeof value === 'string' ? value.trim() : '');

// .tile-more — the "…" affordance. Declared first so the Tile wrapper can
// reference it (& .tile-more) to flip its opacity on hover / playing.
const TileMore = styled.button`
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  color: var(--ink-2);
  background: rgba(255, 255, 255, 0.5);
  opacity: 0;
  transition: opacity 180ms ease, background 180ms ease;
  backdrop-filter: blur(4px);

  &:hover { background: rgba(255, 255, 255, 0.9); color: var(--ink); }

  @media (hover: none) { opacity: 1; }
`;

const TileLogo = styled.span`
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: grid;
  place-items: center;
  background: var(--surface-3);
  color: var(--ink);
  flex-shrink: 0;
  overflow: hidden;
  padding: 0;

  &:has(img) {
    background: transparent;
    box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06);
  }
`;

const TileName = styled.span`
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1.1;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  margin-top: 8px;
`;

// .tile (filled state wrapper) + is-playing / is-starting variants.
const Tile = styled.div`
  position: relative;
  background: var(--surface);
  border-radius: var(--radius);
  aspect-ratio: 1.1 / 1;
  min-height: 116px;
  overflow: hidden;
  transition: transform 180ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;
  box-shadow: var(--shadow-sm);

  &:hover { transform: translateY(-2px); box-shadow: var(--shadow); }
  &:active { transform: translateY(0) scale(0.985); }

  &:hover ${TileMore} { opacity: 1; }

  ${(p) => p.$playing && css`
    background: var(--accent-soft);
    box-shadow:
      0 8px 22px -8px rgba(196, 122, 82, 0.4),
      inset 0 0 0 1px rgba(196, 122, 82, 0.25);

    ${TileName} { color: var(--ink); }
    ${TileLogo} { background: var(--surface); color: var(--accent); }
    ${TileMore} { color: var(--accent); background: var(--surface); opacity: 1; }
  `}
  ${(p) => p.$starting && css`
    background: linear-gradient(180deg, var(--accent-soft), var(--surface));
    box-shadow:
      0 6px 18px -10px rgba(196, 122, 82, 0.3),
      inset 0 0 0 1px rgba(196, 122, 82, 0.18);

    ${TileLogo} { background: var(--surface); color: var(--accent); }
    ${TileMore} { opacity: 1; color: var(--accent); background: var(--surface); }
  `}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

const TileMain = styled.button`
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 14px;
  text-align: left;
  color: var(--ink);
`;

const TileRow = styled.span`
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 6px;
  min-height: 44px;
`;

const TileLive = styled.span`
  position: absolute;
  bottom: 12px;
  right: 12px;
  display: inline-flex;
  align-items: center;
  padding: 4px 8px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.65);
  backdrop-filter: blur(4px);
  box-shadow: 0 1px 2px rgba(31, 24, 20, 0.05);
  pointer-events: none;
`;

// .tile.tile-empty — the empty-slot variant. Distinct enough from the filled
// tile to keep as its own styled button.
const TileEmpty = styled.button`
  position: relative;
  background: transparent;
  border-radius: var(--radius);
  aspect-ratio: 1.1 / 1;
  min-height: 116px;
  overflow: hidden;
  transition: transform 180ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;

  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  gap: 10px;
  padding: 14px;
  width: 100%;
  cursor: pointer;
  border: 1.5px dashed var(--border-strong);
  color: var(--ink-2);
  box-shadow: none;
  text-align: center;

  &:hover { background: var(--surface-2); border-color: var(--border-strong); }
  &:active { transform: translateY(0) scale(0.985); }

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

const TileEmptyMid = styled.span`
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--accent);
  box-shadow: var(--shadow-sm);
`;

const TileEmptyLabel = styled.span`
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
`;

const TileEmptyHint = styled.span`
  font-size: 11.5px;
  color: var(--ink-2);
  margin-top: 1px;
  display: block;
`;

// .popover — replace menu.
const Popover = styled.div`
  position: absolute;
  top: 40px;
  right: 8px;
  z-index: 5;
  background: var(--surface);
  border-radius: 14px;
  padding: 5px;
  min-width: 140px;
  box-shadow: 0 14px 36px -10px rgba(31, 24, 20, 0.25), 0 0 0 1px rgba(31, 24, 20, 0.06);
  display: flex;
  flex-direction: column;
  animation: ${pop} 160ms cubic-bezier(.34,.78,.18,1);
  color: var(--ink);

  button {
    padding: 9px 12px;
    font-size: 13.5px;
    font-weight: 500;
    border-radius: 10px;
    text-align: left;
    color: var(--ink);
  }
  button:hover { background: var(--surface-3); }
`;

// .tile-skeleton — a loading placeholder matching the filled tile's shape, so
// the grid doesn't flash empty "add" slots before the presets arrive.
const TileSkeleton = styled.div`
  background: var(--surface);
  border-radius: var(--radius);
  aspect-ratio: 1.1 / 1;
  min-height: 116px;
  overflow: hidden;
  box-shadow: var(--shadow-sm);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 14px;
`;

export function PresetTileSkeleton() {
  return (
    <TileSkeleton aria-hidden="true">
      <Skeleton style={{ width: 44, height: 44 }} $radius="12px" />
      <Skeleton style={{ width: '70%', height: 16 }} $radius="6px" />
    </TileSkeleton>
  );
}

export function PresetTile({ preset, player, active, onPlay, onAssign }) {
  const { t } = useI18n();
  const [menu, setMenu] = useState(false);
  const ref = useRef(null);

  // Close the menu on an outside click.
  useEffect(() => {
    if (!menu) return;
    const fn = (e) => { if (ref.current && !ref.current.contains(e.target)) setMenu(false); };
    document.addEventListener('mousedown', fn);
    document.addEventListener('touchstart', fn);
    return () => {
      document.removeEventListener('mousedown', fn);
      document.removeEventListener('touchstart', fn);
    };
  }, [menu]);

  if (!preset) {
    return (
      <TileEmpty onClick={onAssign}>
        <TileEmptyMid>
          <Icon.plus width="22" height="22" />
        </TileEmptyMid>
        <span>
          <TileEmptyLabel>{t('emptySlot')}</TileEmptyLabel>
          <TileEmptyHint>{t('tapToFill')}</TileEmptyHint>
        </span>
      </TileEmpty>
    );
  }

  const name = clean(preset.name) || '?';

  // Whether this is the preset the player is on is resolved once by the grid (see
  // PresetGrid / activePresetIndex) and passed in as `active`, so a loose name
  // match can't light up several tiles at once. starting/buffering shows a loader
  // on the tile; PLAY_STATE shows the live equalizer.
  const isPlaying = active && player.status === 'playing';
  const isStarting = active && (player.status === 'starting' || player.status === 'buffering');

  return (
    <Tile $playing={isPlaying} $starting={isStarting} ref={ref}>
      <TileMain onClick={onPlay}>
        <TileRow>
          <TileLogo>
            <StationLogo name={name} tuneInId={preset.tuneInId} logo={preset.logo} />
          </TileLogo>
        </TileRow>
        <span>
          <TileName>{name}</TileName>
        </span>
        {isPlaying && (
          <TileLive>
            <EqualizerBars />
          </TileLive>
        )}
        {isStarting && (
          <TileLive>
            <Spinner $tile />
          </TileLive>
        )}
      </TileMain>
      <TileMore
        aria-label={t('replace')}
        onClick={(e) => { e.stopPropagation(); setMenu(m => !m); }}
      >
        <Icon.more width="16" height="16" />
      </TileMore>
      {menu && (
        <Popover role="menu">
          <button onClick={() => { setMenu(false); onAssign(); }}>{t('replace')}</button>
        </Popover>
      )}
    </Tile>
  );
}

export default PresetTile;
