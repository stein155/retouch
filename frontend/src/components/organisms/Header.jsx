import styled from 'styled-components';
import { Icon } from '../atoms/Icon';
import { useI18n } from '../../lib/i18n';

const Hdr = styled.header`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: max(26px, calc(env(safe-area-inset-top) + 10px)) 24px 8px;
  flex-shrink: 0;
  position: relative;
  z-index: 10;
`;

const HdrBrand = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

const HdrBlob = styled.span`
  width: 34px;
  height: 34px;
  border-radius: 50%;
  background: var(--accent-soft);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  line-height: 1;
  padding-top: 1px;
  box-shadow: 0 1px 4px rgba(31, 24, 20, 0.10), 0 0 0 1px rgba(31, 24, 20, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
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
  transition: transform 160ms cubic-bezier(.34,.78,.18,1), box-shadow 200ms ease;

  &:hover { transform: rotate(30deg); box-shadow: var(--shadow); }
`;

// Single speaker driven through the on-box STLocal agent (runs on the speaker
// itself and serves this UI at /). The speaker name comes from its own settings.
export function Header({ onSettings }) {
  const { t } = useI18n();
  return (
    <Hdr>
      <HdrBrand>
        <HdrBlob aria-hidden="true">📻</HdrBlob>
        <HdrTitle>{t('appTitle')}</HdrTitle>
      </HdrBrand>
      <HdrGear onClick={onSettings} aria-label={t('settings')}>
        <Icon.settings width="22" height="22" />
      </HdrGear>
    </Hdr>
  );
}

export default Header;
