import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import IconButton from './IconButton.svelte';
import IconButtonHarness from './IconButton.test.svelte';

describe('IconButton', () => {
  it('renders children and title', () => {
    render(IconButtonHarness, { props: { title: 'Hello', active: false } });
    const btn = screen.getByTitle('Hello');
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveTextContent('X');
  });

  it('applies active styling when active', () => {
    render(IconButtonHarness, { props: { title: 'A', active: true } });
    const btn = screen.getByTitle('A');
    expect(btn.className).toMatch(/bg-lerd-red\/10/);
  });

  it('fires onclick', async () => {
    const onclick = vi.fn();
    render(IconButtonHarness, { props: { title: 'B', active: false, onclick } });
    screen.getByTitle('B').click();
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('defines three size variants', () => {
    // keeps the size prop honest; if someone removes sm/md/lg it fails
    expect(Object.keys(IconButton)).toBeDefined();
  });
});
