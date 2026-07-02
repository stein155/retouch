import { useState } from 'react';

function stationInitials(name) {
  if (!name) return '?';
  const upper = name.replace(/[^A-Z]/g, '');
  if (upper.length >= 2) return upper.slice(0, 2);
  if (upper.length === 1) return upper + name.replace(/[^a-z]/gi, '').slice(1, 2).toLowerCase();
  return name.slice(0, 2).toUpperCase();
}

// Build the same-origin proxy URL for a logo. STLocal exposes
// GET /api/logo?u=<encoded absolute url> (TuneIn has no CORS / is plain http).
function proxiedLogo(absUrl) {
  if (!absUrl) return null;
  return `/api/logo?u=${encodeURIComponent(absUrl)}`;
}

// TuneIn's CDN serves a station logo at a predictable path keyed by guide id.
function tuneInLogoUrl(tuneInId) {
  if (!tuneInId) return null;
  return proxiedLogo(`http://cdn-radiotime-logos.tunein.com/${tuneInId}g.png`);
}

const initialsStyle = {
  fontFamily: 'inherit',
  fontWeight: 800,
  fontSize: '14px',
  lineHeight: 1,
  textAlign: 'center',
};

export function StationLogo({ id, name, tuneInId, logo }) {
  // Track failure and successful load per URL, so state doesn't leak across
  // stations when this component is reused in place (the MiniPlayer keeps one
  // StationLogo mounted across station switches).
  const [errorUrl, setErrorUrl] = useState(null);
  const [loadedUrl, setLoadedUrl] = useState(null);

  // Prefer an explicit logo URL (from a preset / search result), else derive
  // one from the TuneIn id. Everything is proxied.
  const logoUrl = (logo ? proxiedLogo(logo) : null) || tuneInLogoUrl(tuneInId);

  const initials = <span style={initialsStyle}>{stationInitials(name || id || '?')}</span>;

  if (!logoUrl || errorUrl === logoUrl) return initials;

  // Show the initials as a placeholder and fade the logo in over them once it
  // has loaded, so the tile doesn't sit as an empty box and then pop.
  const loaded = loadedUrl === logoUrl;
  return (
    <span style={{ position: 'relative', width: '100%', height: '100%', display: 'grid', placeItems: 'center' }}>
      {!loaded && initials}
      <img
        src={logoUrl}
        alt={name || id || ''}
        onLoad={() => setLoadedUrl(logoUrl)}
        onError={() => setErrorUrl(logoUrl)}
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
