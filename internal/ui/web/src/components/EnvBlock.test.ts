import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import EnvBlock from './EnvBlock.svelte';

describe('EnvBlock', () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn(async () => {}) }
    });
  });

  it('renders sorted KEY=value pairs', () => {
    render(EnvBlock, { props: { vars: { B: 'two', A: 'one' } } });
    const pre = document.querySelector('pre')!;
    expect(pre.textContent).toBe('A=one\nB=two');
  });

  it('uses custom label', () => {
    render(EnvBlock, { props: { vars: { K: 'v' }, label: 'Config' } });
    expect(screen.getByText('Config')).toBeInTheDocument();
  });

  it('copies joined env text to clipboard', async () => {
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(EnvBlock, { props: { vars: { A: '1' } } });
    screen.getByText('Copy').click();
    await Promise.resolve();
    expect(writeText).toHaveBeenCalledWith('A=1');
  });
});
