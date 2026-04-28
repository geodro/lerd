import { render } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Toggle from './Toggle.svelte';

function btnCls(container: HTMLElement): string {
  return container.querySelector('button')!.className;
}
function thumbCls(container: HTMLElement): string {
  return container.querySelector('button > span')!.className;
}

describe('Toggle', () => {
  it('off uses gray background, thumb at left', () => {
    const { container } = render(Toggle, { props: { on: false } });
    expect(btnCls(container)).toMatch(/bg-gray-300|dark:bg-lerd-muted/);
    expect(thumbCls(container)).toMatch(/translate-x-0\.5/);
  });

  it('on uses tone color, thumb at right', () => {
    const { container } = render(Toggle, { props: { on: true, tone: 'teal' } });
    expect(btnCls(container)).toMatch(/bg-teal-500/);
    expect(thumbCls(container)).toMatch(/translate-x-3\.5/);
  });

  it('failing pulses red', () => {
    const { container } = render(Toggle, { props: { on: false, failing: true } });
    expect(btnCls(container)).toMatch(/bg-red-500/);
    expect(btnCls(container)).toMatch(/animate-pulse/);
  });

  it('loading disables the button', () => {
    const { container } = render(Toggle, { props: { on: false, loading: true } });
    expect((container.querySelector('button') as HTMLButtonElement).disabled).toBe(true);
  });

  it('fires onclick', () => {
    const onclick = vi.fn();
    const { container } = render(Toggle, { props: { on: false, onclick } });
    container.querySelector('button')!.click();
    expect(onclick).toHaveBeenCalledOnce();
  });
});
