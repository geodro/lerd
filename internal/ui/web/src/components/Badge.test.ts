import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './Badge.test.svelte';

describe('Badge', () => {
  it('renders span by default', () => {
    render(Harness, { props: { tone: 'running', label: 'running' } });
    const el = screen.getByText('running');
    expect(el.tagName.toLowerCase()).toBe('span');
  });

  it('renders a button when onclick is set', () => {
    const onclick = vi.fn();
    render(Harness, { props: { tone: 'framework', label: 'Laravel', onclick } });
    const el = screen.getByText('Laravel');
    expect(el.tagName.toLowerCase()).toBe('button');
    el.click();
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('shows dot only when dot=true', () => {
    const { container } = render(Harness, { props: { tone: 'running', label: 'up', dot: true } });
    expect(container.querySelector('span.rounded-full')).toBeInTheDocument();
  });

  it('applies tone classes', () => {
    render(Harness, { props: { tone: 'paused', label: 'paused' } });
    expect(screen.getByText('paused').className).toMatch(/text-amber-600|bg-amber-50/);
  });
});
