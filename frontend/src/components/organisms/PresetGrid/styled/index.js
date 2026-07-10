import styled from 'styled-components';

export const SectHead = styled.div`
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 14px;
`;

export const SectTitle = styled.span`
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -0.02em;
`;

export const SectSub = styled.span`
  font-size: 12px;
  color: var(--ink-2);
`;

export const Grid = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
`;
