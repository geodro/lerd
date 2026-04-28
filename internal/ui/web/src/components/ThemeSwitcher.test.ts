import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import ThemeSwitcher from './ThemeSwitcher.svelte';
import { theme } from '$stores/theme';

describe('ThemeSwitcher', () => {
  beforeEach(() => {
    theme.set('auto');
  });

  it('renders three mode buttons', () => {
    render(ThemeSwitcher);
    expect(screen.getByTitle('light')).toBeInTheDocument();
    expect(screen.getByTitle('auto')).toBeInTheDocument();
    expect(screen.getByTitle('dark')).toBeInTheDocument();
  });

  it('marks the active mode with bg class', () => {
    theme.set('dark');
    render(ThemeSwitcher);
    expect(screen.getByTitle('dark').className).toMatch(/bg-gray-200|dark:bg-white\/10/);
    expect(screen.getByTitle('light').className).not.toMatch(/bg-gray-200/);
  });

  it('updates the theme store on click', () => {
    render(ThemeSwitcher);
    screen.getByTitle('dark').click();
    expect(get(theme)).toBe('dark');
    screen.getByTitle('light').click();
    expect(get(theme)).toBe('light');
  });

  it('is vertical at size="sm" (rail)', () => {
    const { container } = render(ThemeSwitcher, { props: { size: 'sm' } });
    const wrap = container.querySelector('div')!;
    expect(wrap.className).toMatch(/flex-col/);
    expect(wrap.className).not.toMatch(/flex-row/);
  });

  it('is horizontal at size="md" (mobile)', () => {
    const { container } = render(ThemeSwitcher, { props: { size: 'md' } });
    const wrap = container.querySelector('div')!;
    expect(wrap.className).toMatch(/flex-row/);
    expect(wrap.className).not.toMatch(/flex-col/);
  });
});
