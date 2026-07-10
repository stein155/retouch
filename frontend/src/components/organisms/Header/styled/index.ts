import styled from 'styled-components';

// Overlays the top of the scrolling body (which is padded to clear it) so
// content can slide underneath. At rest it's a solid bar; once the body is
// scrolled it turns to a translucent, blurred scrim that fades to nothing at
// its bottom edge — so the content reads through it and dissolves downward.
// pointer-events: none lets scroll/taps fall through the transparent lower
// part; the gear re-enables them for itself.
export const Hdr = styled.header<{ $scrolled?: boolean }>`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: max(26px, calc(env(safe-area-inset-top) + 10px)) 24px 8px;
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  z-index: 10;
  pointer-events: none;
  /* Transparent at rest so the Shell's gradient canvas shows through seamlessly
     (the bar only overlaps the scroll area's empty top padding); the $scrolled
     state below fades in a blurred scrim once content slides underneath. */
  background: transparent;
  transition: background 240ms ease, backdrop-filter 240ms ease;

  ${(p) => p.$scrolled && `
    /* Fully opaque through the title band (so scrolling content is cleanly
       hidden rather than ghosting through it), then a soft fade + blur on the
       bottom edge that eases the first rows in as they slide underneath. */
    background: linear-gradient(
      to bottom,
      rgba(var(--bg-rgb), 1) 0%,
      rgba(var(--bg-rgb), 1) 62%,
      rgba(var(--bg-rgb), 0) 100%
    );
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    -webkit-mask-image: linear-gradient(to bottom, #000 0%, #000 66%, transparent 100%);
    mask-image: linear-gradient(to bottom, #000 0%, #000 66%, transparent 100%);
  `}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

export const HdrBrand = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

export const HdrBlob = styled.span`
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--accent-soft);
  overflow: hidden;
  box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.5);
  flex-shrink: 0;

  img { width: 100%; height: 100%; object-fit: cover; display: block; }
`;

export const HdrTitle = styled.span`
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
`;

export const HdrGear = styled.button`
  margin-left: auto;
  width: 40px;
  height: 40px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow-sm);
  pointer-events: auto;
  transition: transform 160ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;

  &:hover { transform: rotate(30deg); box-shadow: var(--shadow); }
`;
