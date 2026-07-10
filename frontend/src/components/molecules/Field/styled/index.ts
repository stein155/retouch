import styled from 'styled-components';

// Settings-form primitives, copied verbatim from styles.css. These compose the
// SettingsSheet organism. The .field-row + .field-row border-top lives on
// FieldCard so stacked rows divide correctly.

export const Form = styled.div`
  display: flex;
  flex-direction: column;
`;

export const FormSection = styled.div`
  font-size: 12.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ink-3);
  margin: 0 0 10px 2px;
`;

export const FieldHint = styled.div<{ $error?: boolean }>`
  font-size: 12.5px;
  color: var(--ink-3);
  margin: 8px 2px 0;
  line-height: 1.4;

  ${(p) => p.$error && `color: #d44030;`}
`;

export const FieldCard = styled.div`
  background: var(--surface);
  border-radius: 16px;
  padding: 4px 16px;
  box-shadow: var(--shadow-sm);
`;

export const FieldRow = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 0;

  & + & { border-top: 1px solid rgba(31, 24, 20, 0.07); }
`;

export const FieldRowLabel = styled.label`
  font-size: 14.5px;
  font-weight: 600;
  color: var(--ink-2);
  flex-shrink: 0;
  min-width: 78px;
`;

export const FieldRowInput = styled.input`
  flex: 1;
  min-width: 0;
  text-align: right;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  background: transparent;
  border: none;
  outline: none;

  &::placeholder { color: var(--ink-3); font-weight: 400; }
`;

export const FieldRowValue = styled.span`
  flex: 1;
  min-width: 0;
  text-align: right;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
`;

// .field as the select base (the settings-sheet variant) + .select.
export const Select = styled.select`
  width: 100%;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  background: var(--surface);
  border: none;
  border-radius: 14px;
  padding: 14px 16px;
  box-shadow: var(--shadow-sm);
  outline: none;

  appearance: none;
  -webkit-appearance: none;
  padding-right: 44px;
  cursor: pointer;
`;

export const SelectWrap = styled.div`
  position: relative;
`;

export const SelectChev = styled.span`
  position: absolute;
  right: 14px;
  top: 50%;
  transform: translateY(-50%) rotate(90deg);
  color: var(--ink-3);
  pointer-events: none;
  display: grid;
  place-items: center;
`;

export const SetEyebrow = styled.div`
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-3);
`;

export const BassCard = styled.div`
  background: var(--surface);
  border-radius: 16px;
  padding: 16px 18px 14px;
  box-shadow: var(--shadow-sm);
`;

export const BassHead = styled.div`
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 14px;
`;

export const BassName = styled.span`
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.015em;
`;

export const BassVal = styled.span<{ $set?: boolean }>`
  font-size: 17px;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
  color: var(--ink-3);

  ${(p) => p.$set && `color: var(--accent);`}
`;

export const BassScale = styled.div`
  display: flex;
  justify-content: space-between;
  margin-top: 9px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-3);
  font-variant-numeric: tabular-nums;
`;
