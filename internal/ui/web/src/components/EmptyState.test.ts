import { render, screen } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import Harness from './EmptyState.test.svelte';

describe('EmptyState', () => {
  it('renders the title', () => {
    render(Harness, { props: { title: 'Nothing here' } });
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
  });

  it('renders the hint snippet when provided', () => {
    render(Harness, { props: { title: 'Nothing', withHint: true } });
    expect(screen.getByText('Some hint text')).toBeInTheDocument();
  });

  it('omits the hint when not provided', () => {
    render(Harness, { props: { title: 'Nothing' } });
    expect(screen.queryByText('Some hint text')).not.toBeInTheDocument();
  });
});
