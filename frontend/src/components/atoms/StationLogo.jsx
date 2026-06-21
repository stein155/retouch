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

export function StationLogo({ id, name, tuneInId, logo }) {
  const [imgError, setImgError] = useState(false);

  // Prefer an explicit logo URL (from a preset / search result), else derive
  // one from the TuneIn id. Everything is proxied.
  const logoUrl = (logo ? proxiedLogo(logo) : null) || tuneInLogoUrl(tuneInId);

  if (logoUrl && !imgError) {
    return (
      <img
        src={logoUrl}
        alt={name || id || ''}
        onError={() => setImgError(true)}
        style={{
          width: '100%',
          height: '100%',
          objectFit: 'contain',
          borderRadius: 'inherit',
        }}
      />
    );
  }

  // Fallback: initials
  return (
    <span style={{
      fontFamily: 'inherit',
      fontWeight: 800,
      fontSize: '14px',
      lineHeight: 1,
      textAlign: 'center',
    }}>
      {stationInitials(name || id || '?')}
    </span>
  );
}

export default StationLogo;
