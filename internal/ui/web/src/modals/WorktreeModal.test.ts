import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import Harness from './WorktreeModal.test.svelte';
import type { Site } from '$stores/sites';

function siteWith(worktrees: Site['worktrees']): Site {
  return { domain: 'acme.test', branch: 'main', worktrees };
}

describe('WorktreeModal', () => {
  it('lists each worktree with its branch and domain', () => {
    render(Harness, {
      props: {
        site: siteWith([
          { branch: 'feat-a', domain: 'feat-a.acme.test' },
          { branch: 'feat-b', domain: 'feat-b.acme.test', db_isolated: true, db_database: 'acme_feat_b' }
        ])
      }
    });
    expect(screen.getByText('feat-a')).toBeTruthy();
    expect(screen.getByText('feat-b')).toBeTruthy();
    expect(screen.getByText('feat-a.acme.test')).toBeTruthy();
  });

  it('shows an empty state when there are no worktrees', () => {
    render(Harness, { props: { site: siteWith([]) } });
    // The "Add worktree" footer button is still present.
    expect(screen.getAllByRole('button').some((b) => /add worktree/i.test(b.textContent || ''))).toBe(
      true
    );
  });

  it('opens an inline remove confirm with a drop-database checkbox for isolated worktrees', async () => {
    render(Harness, {
      props: {
        site: siteWith([{ branch: 'feat-a', domain: 'feat-a.acme.test', db_isolated: true, db_database: 'acme_feat_a' }])
      }
    });
    const removeBtn = screen.getByRole('button', { name: 'Remove' });
    await fireEvent.click(removeBtn);
    // Force checkbox + drop-database checkbox (with the DB name) appear.
    const checkboxes = screen.getAllByRole('checkbox') as HTMLInputElement[];
    expect(checkboxes.length).toBe(2);
    expect(screen.getByText(/acme_feat_a/)).toBeTruthy();
  });

  it('switches to the add form when "Add worktree" is clicked', async () => {
    render(Harness, { props: { site: siteWith([]) } });
    const addBtn = screen.getAllByRole('button').find((b) => /add worktree/i.test(b.textContent || ''));
    await fireEvent.click(addBtn!);
    // The branch-mode radios show up in the add view.
    expect(screen.getAllByRole('radio').length).toBe(2);
  });
});
