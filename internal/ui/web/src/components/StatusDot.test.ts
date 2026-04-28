import { render } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import StatusDot from './StatusDot.svelte';

function cls(container: HTMLElement): string {
  return container.querySelector('span')!.className;
}

describe('StatusDot', () => {
  it('uses emerald for green', () => {
    const { container } = render(StatusDot, { props: { color: 'green' } });
    expect(cls(container)).toMatch(/bg-emerald-500/);
  });

  it('uses red for red', () => {
    const { container } = render(StatusDot, { props: { color: 'red' } });
    expect(cls(container)).toMatch(/bg-red-500/);
  });

  it('adds animate-pulse when pulse=true', () => {
    const { container } = render(StatusDot, { props: { color: 'green', pulse: true } });
    expect(cls(container)).toMatch(/animate-pulse/);
  });

  it('respects size prop', () => {
    const { container } = render(StatusDot, { props: { color: 'green', size: 'xs' } });
    expect(cls(container)).toMatch(/w-1\.5/);
  });
});
