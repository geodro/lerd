import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './ActionButton.test.svelte';

describe('ActionButton', () => {
  it('renders its title and an icon', () => {
    render(Harness, { props: { title: 'Refresh' } });
    const btn = screen.getByTitle('Refresh');
    expect(btn).toBeInTheDocument();
    expect(btn.querySelector('svg')).toBeInTheDocument();
  });

  it('shows a spinner when loading', () => {
    render(Harness, { props: { title: 'X', loading: true } });
    const btn = screen.getByTitle('X');
    expect(btn.querySelector('svg.animate-spin')).toBeInTheDocument();
  });

  it('applies danger tone', () => {
    render(Harness, { props: { title: 'Y', tone: 'danger' } });
    expect(screen.getByTitle('Y').className).toMatch(/text-red-500/);
  });

  it('applies success tone', () => {
    render(Harness, { props: { title: 'Z', tone: 'success' } });
    expect(screen.getByTitle('Z').className).toMatch(/text-emerald-600/);
  });

  it('disables the button and dims it', () => {
    render(Harness, { props: { title: 'D', disabled: true } });
    const btn = screen.getByTitle('D') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(btn.className).toMatch(/disabled:opacity-40/);
  });

  it('forwards clicks', () => {
    const onclick = vi.fn();
    render(Harness, { props: { title: 'C', onclick } });
    screen.getByTitle('C').click();
    expect(onclick).toHaveBeenCalledOnce();
  });
});
