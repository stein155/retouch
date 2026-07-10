import styled from 'styled-components';
import { Icon } from '../../atoms/Icon';
import { Toggle } from '../../atoms/Toggle';
import { FieldRow } from '../Field';

// .spk-row — a .field-row with the multiroom gap override.
const SpkRow = styled(FieldRow)`
  gap: 12px;
`;

const SpkIcon = styled.span`
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: rgba(196, 122, 82, 0.12);
  color: var(--accent);
`;

const SpkText = styled.span`
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
`;

const SpkName = styled.span`
  font-size: 14.5px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.01em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

const SpkModel = styled.span`
  font-size: 11.5px;
  font-weight: 500;
  color: var(--ink-3);
  margin-top: 1px;
`;

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
