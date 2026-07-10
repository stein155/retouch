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

// --- Browse ----------------------------------------------------------------

// Top bar of the browse view: an optional back button + the "surprise me" action.
export const BrowseBar = styled.div`
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 20px 10px;
`;

export const BrowseBack = styled.button`
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-radius: 12px;
  background: var(--surface-3);
  color: var(--ink);
  font-size: 13px;
  font-weight: 600;

  &:hover { background: var(--ink); color: var(--bg); }
`;


// A drill-down category row (genre / region / …).
export const CatRow = styled.button`
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 14px 20px;
  color: var(--ink);
  font-size: 15px;
  font-weight: 600;
  text-align: left;
  border-bottom: 1px solid var(--surface-3);

  &:hover { background: var(--surface); }

  span:last-child { color: var(--ink-3); }
`;
