import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './DetailButton.test.svelte';

describe('DetailButton', () => {
  it('renders a button by default', () => {
    render(Harness, { props: { label: 'Click' } });
    const btn = screen.getByText('Click').closest('button');
    expect(btn).toBeInTheDocument();
  });

  it('renders an anchor when href is set', () => {
    render(Harness, { props: { label: 'Go', href: 'https://example.com' } });
    const link = screen.getByText('Go').closest('a');
    expect(link).toBeInTheDocument();
    expect(link?.getAttribute('href')).toBe('https://example.com');
  });

  it('fires onclick when clicked', () => {
    const onclick = vi.fn();
    render(Harness, { props: { label: 'Go', onclick } });
    screen.getByText('Go').click();
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('applies danger tone classes', () => {
    render(Harness, { props: { label: 'Rm', tone: 'danger' } });
    const btn = screen.getByText('Rm').closest('button')!;
    expect(btn.className).toMatch(/hover:text-red-600|hover:bg-red-50/);
  });

  it('shows spinner when loading', () => {
    const { container } = render(Harness, { props: { label: 'x', loading: true } });
    expect(container.querySelector('svg.animate-spin')).toBeInTheDocument();
  });

  it('hides children when loading so the button does not grow', () => {
    render(Harness, { props: { label: 'Start', loading: true } });
    expect(screen.queryByText('Start')).not.toBeInTheDocument();
  });

  it('is disabled when disabled=true', () => {
    render(Harness, { props: { label: 'd', disabled: true } });
    const btn = screen.getByText('d').closest('button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });
});
