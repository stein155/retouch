import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { useI18n } from '../../lib/i18n';

// Overlays the top of the scrolling body (which is padded to clear it) so
// content can slide underneath. At rest it's a solid bar; once the body is
// scrolled it turns to a translucent, blurred scrim that fades to nothing at
// its bottom edge — so the content reads through it and dissolves downward.
// pointer-events: none lets scroll/taps fall through the transparent lower
// part; the gear re-enables them for itself.
const Hdr = styled.header`
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
  background: var(--bg);
  transition: background 240ms ease, backdrop-filter 240ms ease;

  ${(p) => p.$scrolled && `
    background: linear-gradient(
      to bottom,
      rgba(241, 230, 203, 0.9) 0%,
      rgba(241, 230, 203, 0.62) 46%,
      rgba(241, 230, 203, 0) 100%
    );
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
    -webkit-mask-image: linear-gradient(to bottom, #000 0%, #000 50%, transparent 100%);
    mask-image: linear-gradient(to bottom, #000 0%, #000 50%, transparent 100%);
  `}

  @media (prefers-reduced-motion: reduce) { transition: none; }
`;

const HdrBrand = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

const HdrBlob = styled.span`
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--accent-soft);
  overflow: hidden;
  box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.5);
  flex-shrink: 0;

  img { width: 100%; height: 100%; object-fit: cover; display: block; }
`;

const HdrTitle = styled.span`
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
`;

const HdrGear = styled.button`
  margin-left: auto;
  width: 40px;
  height: 40px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.6);
  color: var(--ink);
  box-shadow: var(--shadow-sm);
  pointer-events: auto;
  transition: transform 160ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;

  &:hover { transform: rotate(30deg); box-shadow: var(--shadow); }
`;

// Single speaker driven through the on-box STLocal agent (runs on the speaker
// itself and serves this UI at /). The speaker name comes from its own settings.
export function Header({ onSettings, scrolled }) {
  const { t } = useI18n();
  return (
    <Hdr $scrolled={scrolled}>
      <HdrBrand>
        <HdrBlob aria-hidden="true"><img src="/badge.png" alt="" width="40" height="40" /></HdrBlob>
        <HdrTitle>{t('appTitle')}</HdrTitle>
      </HdrBrand>
      <HdrGear onClick={onSettings} aria-label={t('settings')}>
        <Icon.settings width="22" height="22" />
      </HdrGear>
    </Hdr>
  );
}

export default Header;
