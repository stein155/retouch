import { Icon } from '../../atoms/Icon';
import { Toggle } from '../../atoms/Toggle';
import type { FoundSpeaker } from '../../../lib/types';
import { SpkRow, SpkIcon, SpkText, SpkName, SpkModel } from './styled';

interface Props {
  speaker: FoundSpeaker;
  disabled?: boolean;
  onToggle: () => void;
}

export function SpeakerRow({ speaker, disabled, onToggle }: Props) {
  const sp = speaker;
  return (
    <SpkRow>
      <SpkIcon><Icon.speaker width="20" height="20" /></SpkIcon>
      <SpkText>
        <SpkName>{sp.name || 'SoundTouch'}</SpkName>
        {sp.model && <SpkModel>{sp.model}</SpkModel>}
      </SpkText>
      <Toggle
        type="button"
        on={sp.grouped}
        aria-label={sp.name || sp.ip}
        disabled={disabled}
        onClick={onToggle}
      />
    </SpkRow>
  );
}

export default SpeakerRow;
