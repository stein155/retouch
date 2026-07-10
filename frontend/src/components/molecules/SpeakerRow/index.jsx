import { Icon } from '../../atoms/Icon';
import { Toggle } from '../../atoms/Toggle';
import { SpkRow, SpkIcon, SpkText, SpkName, SpkModel } from './styled';

export function SpeakerRow({ speaker, disabled, onToggle }) {
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
