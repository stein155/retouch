import styled from 'styled-components';
import { FieldRow } from '../../Field';

// .spk-row — a .field-row with the multiroom gap override.
export const SpkRow = styled(FieldRow)`
  gap: 12px;
`;

export const SpkIcon = styled.span`
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: rgba(196, 122, 82, 0.12);
  color: var(--accent);
`;

export const SpkText = styled.span`
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
`;

export const SpkName = styled.span`
  font-size: 14.5px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.01em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

export const SpkModel = styled.span`
  font-size: 11.5px;
  font-weight: 500;
  color: var(--ink-3);
  margin-top: 1px;
`;
