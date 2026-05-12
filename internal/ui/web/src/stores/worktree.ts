import { apiFetch, apiJson } from '$lib/api';

export interface LabeledOption {
  value: string;
  label: string;
}

export interface WorktreeOptions {
  local_branches: string[];
  remote_branches: string[];
  default_branch_label: string;
  build_options: LabeledOption[];
  build_default: string;
  db_options: LabeledOption[];
  can_migrate: boolean;
  error?: string;
}

export async function worktreeOptions(domain: string, branch = ''): Promise<WorktreeOptions> {
  const qs = new URLSearchParams({ domain });
  if (branch) qs.set('branch', branch);
  return apiJson<WorktreeOptions>('/api/sites/worktree-options?' + qs.toString());
}

export interface RemoveWorktreeOpts {
  force?: boolean;
  dropDB?: boolean;
}

export async function removeWorktree(
  domain: string,
  branch: string,
  opts: RemoveWorktreeOpts = {}
): Promise<{ ok: boolean; error?: string }> {
  const qs = new URLSearchParams({ branch });
  if (opts.force) qs.set('force', '1');
  if (opts.dropDB) qs.set('drop_db', '1');
  try {
    const res = await apiFetch(
      `/api/sites/${encodeURIComponent(domain)}/worktree:remove?` + qs.toString(),
      { method: 'POST' }
    );
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export interface AddWorktreeParams {
  newBranch?: string;
  existingBranch?: string;
  baseRef?: string;
  db?: string;
  migrate?: boolean;
  build?: string;
}

export interface WorktreeAddEvent {
  line?: string;
  done?: boolean;
  ok?: boolean;
  branch?: string;
  domain?: string;
  error?: string;
}

// streamWorktreeAdd POSTs to the SSE endpoint and invokes onEvent for each
// progress line and the final done payload. Mirrors streamLinkSite.
export async function streamWorktreeAdd(
  domain: string,
  params: AddWorktreeParams,
  onEvent: (e: WorktreeAddEvent) => void
): Promise<void> {
  const qs = new URLSearchParams({ domain });
  if (params.newBranch) qs.set('new_branch', params.newBranch);
  if (params.existingBranch) qs.set('existing_branch', params.existingBranch);
  if (params.baseRef) qs.set('base_ref', params.baseRef);
  if (params.db) qs.set('db', params.db);
  if (params.migrate) qs.set('migrate', '1');
  if (params.build) qs.set('build', params.build);

  const res = await apiFetch('/api/sites/worktree-add?' + qs.toString(), { method: 'POST' });
  if (!res.body) throw new Error('no response body');
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let eventType = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const lines = buf.split('\n');
    buf = lines.pop() ?? '';
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7);
      } else if (line.startsWith('data: ')) {
        const payload = line.slice(6);
        if (eventType === 'done') {
          try {
            const r = JSON.parse(payload) as {
              ok?: boolean;
              branch?: string;
              domain?: string;
              error?: string;
            };
            onEvent({ done: true, ok: Boolean(r.ok), branch: r.branch, domain: r.domain, error: r.error });
          } catch {
            onEvent({ done: true, ok: false, error: 'bad done payload' });
          }
        } else {
          onEvent({ line: payload });
        }
        eventType = '';
      } else if (line === '') {
        eventType = '';
      }
    }
  }
}
