import { render, screen } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import Harness from './ListPanelHeader.test.svelte';

describe('ListPanelHeader', () => {
  it('renders title uppercased via class', () => {
    render(Harness, { props: { title: 'Sites' } });
    const el = screen.getByText('Sites');
    expect(el.className).toMatch(/uppercase/);
  });

  it('renders actions snippet when provided', () => {
    render(Harness, { props: { title: 'Sites', withActions: true } });
    expect(screen.getByTitle('act')).toBeInTheDocument();
  });

  it('omits actions container when no snippet', () => {
    render(Harness, { props: { title: 'Sites' } });
    expect(screen.queryByTitle('act')).not.toBeInTheDocument();
  });
});
