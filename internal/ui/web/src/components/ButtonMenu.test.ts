import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './ButtonMenu.test.svelte';
import type { ButtonMenuAction } from './ButtonMenu.svelte';

function action(over: Partial<ButtonMenuAction> = {}): ButtonMenuAction {
  return { id: 'x', label: 'X', onclick: () => {}, ...over };
}

describe('ButtonMenu', () => {
  it('renders nothing when actions is empty', () => {
    const { container } = render(Harness, { props: { actions: [] } });
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelector('a')).toBeNull();
  });

  it('renders a single button without a caret toggle when given one action', () => {
    render(Harness, { props: { actions: [action({ id: 'a', label: 'Solo' })] } });
    expect(screen.getByText('Solo')).toBeInTheDocument();
    expect(screen.queryByTestId('button-menu-toggle')).not.toBeInTheDocument();
  });

  it('renders an anchor when the only action has an href', () => {
    render(Harness, {
      props: { actions: [action({ id: 'a', label: 'Open', href: 'https://example.com' })] }
    });
    const link = screen.getByText('Open').closest('a');
    expect(link).toBeInTheDocument();
    expect(link?.getAttribute('href')).toBe('https://example.com');
  });

  it('fires onclick for the primary button in a split-button', () => {
    const fn = vi.fn();
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary', onclick: fn }),
          action({ id: 's', label: 'Second' })
        ]
      }
    });
    screen.getByText('Primary').click();
    expect(fn).toHaveBeenCalledOnce();
  });

  it('toggles the menu when the caret is clicked', async () => {
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary' }),
          action({ id: 's', label: 'Second' })
        ]
      }
    });
    expect(screen.queryByTestId('button-menu-list')).not.toBeInTheDocument();
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    expect(screen.getByTestId('button-menu-list')).toBeInTheDocument();
    expect(screen.getByText('Second')).toBeInTheDocument();
  });

  it('does not show the primary action inside the dropdown', async () => {
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'PrimaryOnly' }),
          action({ id: 's', label: 'OtherItem' })
        ]
      }
    });
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    const list = screen.getByTestId('button-menu-list');
    expect(list.textContent).toContain('OtherItem');
    expect(list.textContent).not.toContain('PrimaryOnly');
  });

  it('runs a menu item onclick and closes the menu', async () => {
    const fn = vi.fn();
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary' }),
          action({ id: 's', label: 'Second', onclick: fn })
        ]
      }
    });
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    await fireEvent.click(screen.getByText('Second'));
    expect(fn).toHaveBeenCalledOnce();
    expect(screen.queryByTestId('button-menu-list')).not.toBeInTheDocument();
  });

  it('closes the menu when Escape is pressed', async () => {
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary' }),
          action({ id: 's', label: 'Second' })
        ]
      }
    });
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    expect(screen.getByTestId('button-menu-list')).toBeInTheDocument();
    await fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId('button-menu-list')).not.toBeInTheDocument();
  });

  it('closes the menu on outside mousedown', async () => {
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary' }),
          action({ id: 's', label: 'Second' })
        ]
      }
    });
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    expect(screen.getByTestId('button-menu-list')).toBeInTheDocument();
    await fireEvent.mouseDown(document.body);
    expect(screen.queryByTestId('button-menu-list')).not.toBeInTheDocument();
  });

  it('disables the primary button and menu items when busy', async () => {
    const fn = vi.fn();
    render(Harness, {
      props: {
        busy: true,
        actions: [
          action({ id: 'p', label: 'Primary', onclick: fn }),
          action({ id: 's', label: 'Second', onclick: fn })
        ]
      }
    });
    const buttons = screen.getAllByRole('button');
    const primary = buttons[0];
    expect((primary as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    const item = screen.getByText('Second').closest('button') as HTMLButtonElement;
    expect(item.disabled).toBe(true);
  });

  it('respects per-action disabled flag in the menu', async () => {
    render(Harness, {
      props: {
        actions: [
          action({ id: 'p', label: 'Primary' }),
          action({ id: 's', label: 'Second', disabled: true })
        ]
      }
    });
    await fireEvent.click(screen.getByTestId('button-menu-toggle'));
    const item = screen.getByText('Second').closest('button') as HTMLButtonElement;
    expect(item.disabled).toBe(true);
  });

  it('shows a spinner on the primary button when busy', () => {
    const { container } = render(Harness, {
      props: {
        busy: true,
        actions: [action({ id: 'p', label: 'Primary' })]
      }
    });
    expect(container.querySelector('svg.animate-spin')).toBeInTheDocument();
  });
});
