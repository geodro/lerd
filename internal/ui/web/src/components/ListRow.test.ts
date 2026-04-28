import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './ListRow.test.svelte';

describe('ListRow', () => {
  it('renders label from children', () => {
    render(Harness, { props: { label: 'DNS' } });
    expect(screen.getByText('DNS')).toBeInTheDocument();
  });

  it('fires onclick', () => {
    const onclick = vi.fn();
    render(Harness, { props: { label: 'X', onclick } });
    screen.getByText('X').parentElement!.click();
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('applies active style class', () => {
    const { container } = render(Harness, { props: { label: 'A', active: true } });
    expect(container.querySelector('button')!.className).toMatch(/bg-lerd-red/);
  });

  it('omits leading when not provided', () => {
    render(Harness, { props: { label: 'N' } });
    expect(screen.queryByTestId('lead')).not.toBeInTheDocument();
  });

  it('renders leading and trailing snippets', () => {
    render(Harness, { props: { label: 'N', withLeading: true, withTrailing: true } });
    expect(screen.getByTestId('lead')).toBeInTheDocument();
    expect(screen.getByTestId('tail')).toBeInTheDocument();
  });
});
