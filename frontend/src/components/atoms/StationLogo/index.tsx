import type * as React from 'react';
import { useState, useEffect, useCallback, useRef } from 'react';

type Props = {
  id?: string | null;
  name?: string;
  tuneInId?: string | null;
  logo?: string;
};

type Status = 'loading' | 'loaded' | 'error';

function stationInitials(name: string): string {
  if (!name) return '?';
  const upper = name.replace(/[^A-Z]/g, '');
  if (upper.length >= 2) return upper.slice(0, 2);
  if (upper.length === 1) return upper + name.replace(/[^a-z]/gi, '').slice(1, 2).toLowerCase();
  return name.slice(0, 2).toUpperCase();
}

// Build the same-origin proxy URL for a logo. STLocal exposes
// GET /api/logo?u=<encoded absolute url> (TuneIn has no CORS / is plain http).
function proxiedLogo(absUrl?: string | null): string | null {
  if (!absUrl) return null;
  return `/api/logo?u=${encodeURIComponent(absUrl)}`;
}

// TuneIn's CDN serves a station logo at a predictable path keyed by guide id.
function tuneInLogoUrl(tuneInId?: string | null): string | null {
  if (!tuneInId) return null;
  return proxiedLogo(`http://cdn-radiotime-logos.tunein.com/${tuneInId}g.png`);
}

const initialsStyle: React.CSSProperties = {
  fontFamily: 'inherit',
  fontWeight: 800,
  fontSize: '14px',
  lineHeight: 1,
  textAlign: 'center',
};

export function StationLogo({ id, name, tuneInId, logo }: Props) {
  // Prefer an explicit logo URL (from a preset / search result), else derive
  // one from the TuneIn id. Everything is proxied.
  const logoUrl = (logo ? proxiedLogo(logo) : null) || tuneInLogoUrl(tuneInId);

  // 'loading' | 'loaded' | 'error'. Keyed off logoUrl via the effect below so
  // state doesn't leak across stations when this component is reused in place
  // (the MiniPlayer keeps one StationLogo mounted across station switches).
  const [status, setStatus] = useState<'loading' | 'loaded' | 'error'>('loading');
  const imgRef = useRef<HTMLImageElement | null>(null);

  // Ref callback: fires when the <img> mounts. This catches a cached image whose
  // load event beats React attaching onLoad — including the case the effect below
  // misses, where the previous render was in the error state (no <img> mounted),
  // so switching to a new, already-cached logo would otherwise sit invisible.
  const setImg = useCallback((img: HTMLImageElement | null) => {
    imgRef.current = img;
    if (!img || !img.complete) return;
    if (img.naturalWidth === 0) setStatus('error');
    else requestAnimationFrame(() => setStatus('loaded'));
  }, []);

  // Reset on URL change and fade the logo in reliably. On the speaker's LAN the
  // proxied logo often loads within a single frame; flipping opacity 0 -> 1 in
  // that same frame gives the transition no painted "from" state, so it snaps
  // in ("pops") instead of fading. Deferring the switch to a rAF guarantees the
  // opacity-0 frame is painted first. We also catch images that were already
  // cached (img.complete) before onLoad could attach, so they fade too rather
  // than sitting invisible behind the initials.
  useEffect(() => {
    setStatus('loading');
    if (!logoUrl) return undefined;
    const img = imgRef.current;
    if (!img || !img.complete) return undefined;
    if (img.naturalWidth === 0) {
      setStatus('error');
      return undefined;
    }
    const raf = requestAnimationFrame(() => setStatus('loaded'));
    return () => cancelAnimationFrame(raf);
  }, [logoUrl]);

  const initials = <span style={initialsStyle}>{stationInitials(name || id || '?')}</span>;

  if (!logoUrl || status === 'error') return initials;

  // Show the initials as a placeholder and fade the logo in over them once it
  // has loaded, so the tile doesn't sit as an empty box and then pop.
  const loaded = status === 'loaded';
  return (
    <span style={{ position: 'relative', width: '100%', height: '100%', display: 'grid', placeItems: 'center' }}>
      {!loaded && initials}
      <img
        ref={setImg}
        src={logoUrl}
        alt={name || id || ''}
        onLoad={() => requestAnimationFrame(() => setStatus('loaded'))}
        onError={() => setStatus('error')}
        style={{
          position: 'absolute',
          inset: 0,
          width: '100%',
          height: '100%',
          objectFit: 'contain',
          borderRadius: 'inherit',
          opacity: loaded ? 1 : 0,
          transition: 'opacity 260ms ease',
        }}
      />
    </span>
  );
}

export default StationLogo;
