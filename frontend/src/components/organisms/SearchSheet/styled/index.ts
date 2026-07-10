import styled from 'styled-components';

export const SheetSearch = styled.div`
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 20px 10px;
  padding: 14px 16px;
  background: var(--surface);
  border-radius: 16px;
  color: var(--ink-2);
  box-shadow: var(--shadow-sm);
  transition: box-shadow 200ms ease;

  &:focus-within { box-shadow: var(--shadow); }

  input {
    flex: 1;
    background: transparent;
    border: 0;
    outline: none;
    color: var(--ink);
    font-size: 15px;
    font-weight: 500;
  }
  input::placeholder { color: var(--ink-3); font-weight: 400; }
`;

export const SheetClear = styled.button`
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  color: var(--ink-2);
  background: var(--surface-3);

  &:hover { background: var(--ink); color: var(--bg); }
`;

export const SheetRows = styled.div`
  display: flex;
  flex-direction: column;
`;

export const SheetEmpty = styled.div`
  padding: 60px 20px;
  text-align: center;
  color: var(--ink-2);
  font-size: 13px;
  font-weight: 500;
`;

export const SheetEmptyQ = styled.div`
  color: var(--ink);
  font-size: 18px;
  font-weight: 700;
  margin-bottom: 8px;
`;
