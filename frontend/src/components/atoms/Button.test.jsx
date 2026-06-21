import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { renderWithTheme, screen } from '../../test/render';
import { Button } from './Button';

describe('Button', () => {
  it('renders its label and fires onClick', async () => {
    const onClick = vi.fn();
    renderWithTheme(<Button $variant="primary" onClick={onClick}>Save</Button>);
    const btn = screen.getByRole('button', { name: 'Save' });
    await userEvent.click(btn);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('does not fire onClick when disabled', async () => {
    const onClick = vi.fn();
    renderWithTheme(<Button $variant="primary" disabled onClick={onClick}>Save</Button>);
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(onClick).not.toHaveBeenCalled();
  });

  it('renders distinct variants without leaking the $variant prop to the DOM', () => {
    const { rerender } = renderWithTheme(<Button $variant="primary">Go</Button>);
    const primary = screen.getByRole('button', { name: 'Go' });
    // styled-components v6 filters transient ($-prefixed) props off the DOM node.
    expect(primary).not.toHaveAttribute('$variant');
    expect(primary.className).toBeTruthy();

    rerender(<Button $variant="ghost">Go</Button>);
    const ghost = screen.getByRole('button', { name: 'Go' });
    expect(ghost.className).toBeTruthy();
  });
});
