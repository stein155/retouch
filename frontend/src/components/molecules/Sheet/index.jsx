import { Icon } from '../../atoms/Icon';
import { SheetHdr, SheetBack, SheetTitle, SheetHeadline } from './styled';

export {
  SheetScrim,
  SheetEl,
  SheetHandle,
  SheetHdr,
  SheetBack,
  SheetTitle,
  SheetHeadline,
  SheetBody,
  Eyebrow,
} from './styled';

// Shared header (back button + eyebrow/title). children = the eyebrow line.
export function SheetHeader({ onClose, closeLabel, children, headline }) {
  return (
    <SheetHdr>
      <SheetBack onClick={onClose} aria-label={closeLabel}>
        <Icon.back width="22" height="22" />
      </SheetBack>
      <SheetTitle>
        {children}
        <SheetHeadline>{headline}</SheetHeadline>
      </SheetTitle>
    </SheetHdr>
  );
}
