import { render, screen } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import StatusPill from './StatusPill.svelte';

describe('StatusPill', () => {
  it('renders label', () => {
    render(StatusPill, { props: { tone: 'ok', label: 'Running' } });
    expect(screen.getByText('Running')).toBeInTheDocument();
  });

  it('applies error tone classes', () => {
    const { container } = render(StatusPill, { props: { tone: 'error', label: 'Down' } });
    expect(container.querySelector('span')!.className).toMatch(/text-red-600|bg-red-100/);
  });

  it('applies warn tone classes', () => {
    const { container } = render(StatusPill, { props: { tone: 'warn', label: 'Stale' } });
    expect(container.querySelector('span')!.className).toMatch(/text-yellow-700|bg-yellow-100/);
  });
});
