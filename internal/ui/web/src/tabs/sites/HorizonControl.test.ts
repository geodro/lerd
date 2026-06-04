import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './HorizonControl.test.svelte';

describe('HorizonControl', () => {
  it('renders the Horizon button', () => {
    render(Harness, { props: {} });
    expect(screen.getByRole('button', { name: 'Horizon' })).toBeInTheDocument();
  });

  it('hides the reload segment when Horizon is not running', () => {
    const { container } = render(Harness, { props: { running: false } });
    expect(container.querySelectorAll('button')).toHaveLength(1);
  });

  it('shows the reload segment only when Horizon is running', () => {
    const { container } = render(Harness, { props: { running: true } });
    expect(container.querySelectorAll('button')).toHaveLength(2);
  });

  it('marks the reload segment pressed and glows only the icon when reload is on', () => {
    const { container } = render(Harness, { props: { running: true, reload: true } });
    const reloadBtn = container.querySelectorAll('button')[1];
    expect(reloadBtn.getAttribute('aria-pressed')).toBe('true');
    // The segment stays the neutral group colour, no emerald fill/ring.
    expect(reloadBtn.className).not.toMatch(/emerald/);
    // The icon carries the green colour and glow.
    const icon = reloadBtn.querySelector('svg');
    expect(icon?.getAttribute('class')).toMatch(/text-emerald-500/);
    expect(icon?.getAttribute('class')).toMatch(/drop-shadow-\[/);
  });

  it('keeps the reload segment visible while a reload toggle is in flight', () => {
    const { container } = render(Harness, { props: { running: false, reloadLoading: true } });
    expect(container.querySelectorAll('button')).toHaveLength(2);
  });

  it('shows the Horizon loading dot (not an off dot) while restarting for reload', () => {
    const { container } = render(Harness, {
      props: { running: true, reload: true, horizonLoading: true, reloadLoading: true }
    });
    const horizonBtn = container.querySelectorAll('button')[0];
    // The amber loading spinner replaces the status dot, same as a starting worker.
    expect(horizonBtn.querySelector('svg.animate-spin')).toBeInTheDocument();
    expect(horizonBtn.querySelector('span.rounded-full')).not.toBeInTheDocument();
    // Both segments stay mounted, no flicker to a single pill.
    expect(container.querySelectorAll('button')).toHaveLength(2);
  });

  it('forwards the Horizon toggle click', () => {
    const onToggle = vi.fn();
    render(Harness, { props: { onToggle } });
    screen.getByRole('button', { name: 'Horizon' }).click();
    expect(onToggle).toHaveBeenCalledOnce();
  });

  it('forwards the reload toggle click', () => {
    const onToggleReload = vi.fn();
    const { container } = render(Harness, { props: { running: true, onToggleReload } });
    (container.querySelectorAll('button')[1] as HTMLButtonElement).click();
    expect(onToggleReload).toHaveBeenCalledOnce();
  });
});
