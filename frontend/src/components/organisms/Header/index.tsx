import { Icon } from '../../atoms/Icon';
import { useI18n } from '../../../lib/i18n';
import { Hdr, HdrBrand, HdrBlob, HdrTitle, HdrGear } from './styled';

interface Props {
  onSettings: () => void;
  scrolled: boolean;
}

// Single speaker driven through the on-box STLocal agent (runs on the speaker
// itself and serves this UI at /). The speaker name comes from its own settings.
export function Header({ onSettings, scrolled }: Props) {
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
