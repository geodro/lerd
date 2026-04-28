import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './DetailTabs.test.svelte';

describe('DetailTabs', () => {
  it('renders visible tabs and omits hidden ones', () => {
    render(Harness, {
      props: {
        active: 'a',
        tabs: [
          { id: 'a', label: 'A' },
          { id: 'b', label: 'B', hidden: true },
          { id: 'c', label: 'C' }
        ],
        onchange: () => {}
      }
    });
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.getByText('C')).toBeInTheDocument();
    expect(screen.queryByText('B')).not.toBeInTheDocument();
  });

  it('marks the active tab with the accent border', () => {
    render(Harness, {
      props: { active: 'b', tabs: [{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }], onchange: () => {} }
    });
    expect(screen.getByText('B').className).toMatch(/border-lerd-red|text-lerd-red/);
    expect(screen.getByText('A').className).not.toMatch(/border-lerd-red/);
  });

  it('emits onchange with the clicked id', () => {
    const onchange = vi.fn();
    render(Harness, {
      props: { active: 'a', tabs: [{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }], onchange }
    });
    screen.getByText('B').click();
    expect(onchange).toHaveBeenCalledWith('b');
  });
});
