import { render, screen } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './Modal.test.svelte';

describe('Modal', () => {
  it('renders title and body when open', () => {
    render(Harness, { props: { open: true, title: 'Hello', onclose: () => {} } });
    expect(screen.getByText('Hello')).toBeInTheDocument();
    expect(screen.getByTestId('body')).toBeInTheDocument();
  });

  it('renders nothing when closed', () => {
    render(Harness, { props: { open: false, title: 'Hello', onclose: () => {} } });
    expect(screen.queryByText('Hello')).not.toBeInTheDocument();
  });

  it('invokes onclose when the backdrop is clicked', () => {
    const onclose = vi.fn();
    const { container } = render(Harness, { props: { open: true, title: 'X', onclose } });
    const backdrop = container.querySelector('button.absolute.inset-0') as HTMLButtonElement;
    backdrop.click();
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('invokes onclose when Escape is pressed', () => {
    const onclose = vi.fn();
    render(Harness, { props: { open: true, title: 'X', onclose } });
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('renders footer slot when provided', () => {
    render(Harness, { props: { open: true, title: 'X', onclose: () => {}, withFooter: true } });
    expect(screen.getByTestId('footer')).toBeInTheDocument();
  });
});
