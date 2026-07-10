import { Section } from '../../atoms/Section';
import { Icon } from '../../atoms/Icon';
import { useI18n } from '../../../lib/i18n';
import { Browse, BrowseIcon, BrowseText, BrowseTitle, BrowseSub, BrowseCta } from './styled';

export function BrowseRow({ onClick }) {
  const { t } = useI18n();
  return (
    <Section>
      <Browse onClick={onClick}>
        <BrowseIcon><Icon.search width="20" height="20" /></BrowseIcon>
        <BrowseText>
          <BrowseTitle>{t('findNew')}</BrowseTitle>
          <BrowseSub>{t('findNewSub')}</BrowseSub>
        </BrowseText>
        <BrowseCta>→</BrowseCta>
      </Browse>
    </Section>
  );
}

export default BrowseRow;
