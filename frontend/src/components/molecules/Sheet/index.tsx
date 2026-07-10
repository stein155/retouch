import type { ReactNode } from 'react';
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

interface SheetHeaderProps {
  onClose: () => void;
  closeLabel?: string;
  children?: ReactNode;
  headline?: ReactNode;
}

// Shared header (back button + eyebrow/title). children = the eyebrow line.
export function SheetHeader({ onClose, closeLabel, children, headline }: SheetHeaderProps) {
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
