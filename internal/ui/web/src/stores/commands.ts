import { writable, get } from 'svelte/store';
import { apiUrl, apiJson, apiFetch } from '$lib/api';
import { wsMessage } from '$lib/ws';

// Mirrors config.FrameworkCommand on the Go side. Keep field order in step
// with the JSON the API emits; unknown fields are ignored.
export interface Command {
  name: string;
  label: string;
  command: string;
  description?: string;
  output?: 'silent' | 'text' | 'url' | 'terminal';
  confirm?: boolean;
  icon?: string;
  cwd?: string;
}

export async function loadCommands(domain: string, branch = ''): Promise<Command[]> {
  const path = `/api/sites/${encodeURIComponent(domain)}/commands`;
  const q = branch ? `?branch=${encodeURIComponent(branch)}` : '';
  const data = await apiJson<{ commands?: Command[] }>(path + q);
  return data.commands ?? [];
}

export interface RunCallbacks {
  onStdout?: (line: string) => void;
  onStderr?: (line: string) => void;
  onDone?: (info: { exit: number; durationMs: number; url?: string }) => void;
  onError?: (message: string) => void;
  onTerminal?: () => void; // fired when the server spawned a terminal instead of streaming
  signal?: AbortSignal;
}

// runCommand POSTs to /commands/:name/run and parses the SSE response stream.
// The browser EventSource API only supports GET, so we read the response
// body manually. Each SSE event is `event: <name>\n` followed by one or more
// `data: <line>\n`, terminated by a blank line.
export async function runCommand(
  domain: string,
  name: string,
  cb: RunCallbacks = {},
  branch = ''
): Promise<void> {
  const path = `/api/sites/${encodeURIComponent(domain)}/commands/${encodeURIComponent(name)}/run`;
  const q = branch ? `?branch=${encodeURIComponent(branch)}` : '';
  const res = await apiFetch(path + q, { method: 'POST', signal: cb.signal });
  if (!res.ok) {
    cb.onError?.(`${res.status} ${res.statusText}`);
    return;
  }
  // Terminal-mode commands return a small JSON payload instead of an SSE
  // stream. Detect via the response Content-Type and call onTerminal.
  const ct = res.headers.get('Content-Type') || '';
  if (!ct.startsWith('text/event-stream')) {
    try {
      const payload = await res.json();
      if (payload?.terminal) {
        cb.onTerminal?.();
      } else if (payload?.error) {
        cb.onError?.(String(payload.error));
      }
    } catch {
      cb.onError?.('unexpected non-streaming response');
    }
    return;
  }
  await readSSE(res, cb);
}

// runDoctorFix POSTs to the doctor fix endpoint, which runs an allowlisted
// package-manager command (composer install/update, npm install, npm audit fix)
// and streams the same SSE contract as runCommand.
export async function runDoctorFix(
  domain: string,
  key: string,
  cb: RunCallbacks = {},
  branch = ''
): Promise<void> {
  const path = `/api/sites/${encodeURIComponent(domain)}/doctor/fix/${encodeURIComponent(key)}/run`;
  const q = branch ? `?branch=${encodeURIComponent(branch)}` : '';
  const res = await apiFetch(path + q, { method: 'POST', signal: cb.signal });
  if (!res.ok || !(res.headers.get('Content-Type') || '').startsWith('text/event-stream')) {
    try {
      const payload = await res.json();
      cb.onError?.(String(payload?.error ?? `${res.status} ${res.statusText}`));
    } catch {
      cb.onError?.(`${res.status} ${res.statusText}`);
    }
    return;
  }
  await readSSE(res, cb);
}

// readSSE consumes a fetch Response body as the SSE event stream both runners
// emit, dispatching each frame to the callbacks.
async function readSSE(res: Response, cb: RunCallbacks): Promise<void> {
  if (!res.body) {
    cb.onError?.('streaming not supported by this browser');
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let sep = buffer.indexOf('\n\n');
    while (sep !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      dispatchFrame(frame, cb);
      sep = buffer.indexOf('\n\n');
    }
  }
  if (buffer.trim()) dispatchFrame(buffer, cb);
}

function dispatchFrame(frame: string, cb: RunCallbacks) {
  let event = 'message';
  const dataLines: string[] = [];
  for (const raw of frame.split('\n')) {
    if (raw.startsWith('event: ')) event = raw.slice(7).trim();
    else if (raw.startsWith('data: ')) dataLines.push(raw.slice(6));
  }
  const data = dataLines.join('\n');
  switch (event) {
    case 'stdout':
      cb.onStdout?.(data);
      break;
    case 'stderr':
      cb.onStderr?.(data);
      break;
    case 'done':
      try {
        const info = JSON.parse(data);
        cb.onDone?.(info);
      } catch {
        cb.onError?.('malformed done payload: ' + data);
      }
      break;
    case 'error':
      cb.onError?.(data);
      break;
  }
}

// Helper used by the URL panel for output: url commands.
export function apiUrlFor(path: string): string {
  return apiUrl(path);
}

// Global run state used by CommandRunModal (mounted at app root). The
// CommandsDropdown and CommandPalette both publish to this store rather than
// owning their own modal. Lets us run commands from any entry point and have
// the user see the same UI surface.
export type RunLine = { stream: 'stdout' | 'stderr' | 'meta'; text: string };

export type CurrentRun =
  | { kind: 'idle' }
  | { kind: 'confirm'; domain: string; cmd: Command }
  | { kind: 'running'; domain: string; cmd: Command; lines: RunLine[]; started: number }
  | {
      kind: 'done';
      domain: string;
      cmd: Command;
      lines: RunLine[];
      exit: number;
      durationMs: number;
      url?: string;
    };

export const currentRun = writable<CurrentRun>({ kind: 'idle' });
export const runningName = writable<string | null>(null);

let abortCtrl: AbortController | null = null;
let toastTimer: ReturnType<typeof setTimeout> | null = null;
export const runToast = writable<string | null>(null);

function setToast(msg: string, ms = 2400) {
  if (toastTimer) clearTimeout(toastTimer);
  runToast.set(msg);
  toastTimer = setTimeout(() => runToast.set(null), ms);
}

// launchCommand is the single entry point both the dropdown and the palette
// call. If the command has confirm: true and skipConfirm is false, it parks
// in the confirm state so the modal can prompt. Otherwise it executes.
// Refuses if another run is in flight (toast + no-op) so a palette click
// can't clobber an active dropdown run's state.
export function launchCommand(domain: string, cmd: Command, opts: { skipConfirm?: boolean; branch?: string } = {}) {
  const cur = get(currentRun);
  if (cur.kind === 'running') {
    setToast('Another command is running. Wait for it to finish.', 2400);
    return;
  }
  if (cmd.confirm && !opts.skipConfirm) {
    currentRun.set({ kind: 'confirm', domain, cmd });
    return;
  }
  void executeCommand(domain, cmd, opts.branch);
}

export async function executeCommand(domain: string, cmd: Command, branch = '') {
  return runInModal(domain, cmd, branch, (cb) => runCommand(domain, cmd.name, cb, branch));
}

// executeDoctorFix runs a doctor fix (composer update, npm audit fix, …) in the
// same run modal as commands, so the user watches the streamed output. Awaitable
// so the caller can re-check once it finishes; no-ops if a run is already live.
export async function executeDoctorFix(domain: string, key: string, label: string, branch = '') {
  if (get(currentRun).kind === 'running') {
    setToast('Another command is running. Wait for it to finish.', 2400);
    return;
  }
  const cmd: Command = { name: key, label, command: '' };
  return runInModal(domain, cmd, branch, (cb) => runDoctorFix(domain, key, cb, branch));
}

// runInModal drives the shared CommandRunModal state for any streaming runner,
// folding stdout/stderr/done/error into currentRun and persisting history.
async function runInModal(domain: string, cmd: Command, branch: string, run: (cb: RunCallbacks) => Promise<void>) {
  const started = Date.now();
  runningName.set(cmd.name);
  if (cmd.output === 'terminal') {
    currentRun.set({ kind: 'idle' });
  } else {
    currentRun.set({ kind: 'running', domain, cmd, lines: [], started });
  }
  abortCtrl = new AbortController();

  const append = (stream: 'stdout' | 'stderr', text: string) => {
    currentRun.update((s) => {
      if (s.kind !== 'running') return s;
      return { ...s, lines: [...s.lines, { stream, text }] };
    });
  };

  try {
    await run({
      signal: abortCtrl.signal,
      onStdout: (l) => append('stdout', l),
      onStderr: (l) => append('stderr', l),
      onDone: ({ exit, durationMs, url }) => {
        currentRun.update((s) => {
          if (s.kind !== 'running') return s;
          const done = { kind: 'done' as const, domain, cmd, lines: s.lines, exit, durationMs, url };
          saveHistory(domain, cmd.name, done);
          maybeNotifyDone(cmd, domain, exit, durationMs);
          return done;
        });
      },
      onTerminal: () => {
        setToast('Opened ' + (cmd.label || cmd.name) + ' in terminal');
      },
      onError: (msg) => {
        currentRun.update((s) => {
          if (s.kind === 'running') {
            return {
              kind: 'done',
              domain,
              cmd,
              lines: [...s.lines, { stream: 'meta', text: '[error] ' + msg }],
              exit: -1,
              durationMs: Date.now() - started
            };
          }
          setToast('Error: ' + msg, 3000);
          return s;
        });
      }
    });
  } finally {
    runningName.set(null);
    abortCtrl = null;
  }
}

export function closeRun() {
  if (abortCtrl) {
    abortCtrl.abort();
    abortCtrl = null;
  }
  currentRun.set({ kind: 'idle' });
  runningName.set(null);
}

// Cached command lists by domain, populated on demand. Used by the palette
// to show "Run <name> on <domain>" entries across all sites without
// re-fetching on every keystroke.
const commandsBySite = writable<Record<string, Command[]>>({});
export const commandsBySiteStore = { subscribe: commandsBySite.subscribe };

export async function preloadCommandsFor(domains: string[]): Promise<void> {
  const current = get(commandsBySite);
  const missing = domains.filter((d) => !(d in current));
  if (missing.length === 0) return;
  const results = await Promise.allSettled(missing.map((d) => loadCommands(d).then((c) => [d, c] as const)));
  commandsBySite.update((prev) => {
    const next = { ...prev };
    for (const r of results) {
      if (r.status === 'fulfilled') next[r.value[0]] = r.value[1];
    }
    return next;
  });
}

export function clearCommandsCache() {
  commandsBySite.set({});
}

// Persisted last-run snapshot per (domain, name) so closing the modal
// doesn't lose what just happened. The user can re-open the same command
// from the dashboard or palette and see the last output in a "Previous
// run" banner. Bounded to 32 entries to cap localStorage growth.
const HISTORY_KEY = 'lerd-commands-history-v1';
const HISTORY_MAX = 32;

interface HistoryEntry {
  domain: string;
  name: string;
  exit: number;
  durationMs: number;
  lines: RunLine[];
  url?: string;
  finishedAt: number;
}

function loadHistory(): HistoryEntry[] {
  try {
    const raw = localStorage.getItem(HISTORY_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as HistoryEntry[]) : [];
  } catch {
    return [];
  }
}

function saveHistory(domain: string, name: string, run: Extract<CurrentRun, { kind: 'done' }>) {
  try {
    const entries = loadHistory().filter((e) => !(e.domain === domain && e.name === name));
    entries.unshift({
      domain,
      name,
      exit: run.exit,
      durationMs: run.durationMs,
      lines: run.lines,
      url: run.url,
      finishedAt: Date.now()
    });
    localStorage.setItem(HISTORY_KEY, JSON.stringify(entries.slice(0, HISTORY_MAX)));
  } catch {
    /* localStorage may be unavailable (private mode, quota) — non-fatal */
  }
}

export function lastRunFor(domain: string, name: string): HistoryEntry | null {
  for (const e of loadHistory()) {
    if (e.domain === domain && e.name === name) return e;
  }
  return null;
}

// Fire a desktop notification when a long command finishes while the tab is
// hidden. Stays silent when the user is watching the modal (they already
// see it) and never prompts for permission — only uses what was granted via
// notify.ts. The 5s threshold keeps notifications off cache:clear-style
// instant runs.
const NOTIFY_THRESHOLD_MS = 5000;

function maybeNotifyDone(cmd: Command, domain: string, exit: number, durationMs: number) {
  if (typeof document === 'undefined' || typeof Notification === 'undefined') return;
  if (Notification.permission !== 'granted') return;
  if (durationMs < NOTIFY_THRESHOLD_MS && !document.hidden) return;
  try {
    const title = (cmd.label || cmd.name) + (exit === 0 ? ' finished' : ' failed (exit ' + exit + ')');
    const body = domain + ' · ' + durationMs + 'ms';
    if ('serviceWorker' in navigator) {
      void navigator.serviceWorker.ready
        .then((reg) => reg.showNotification(title, { body, tag: 'lerd-cmd-' + domain + '-' + cmd.name, icon: '/icons/icon-192.png' }))
        .catch(() => {
          new Notification(title, { body });
        });
    } else {
      new Notification(title, { body });
    }
  } catch {
    /* notification failures are non-fatal */
  }
}

// Whenever the sites snapshot changes, drop the cached command lists. A new
// site might have been added, or an existing site's .lerd.yaml may have been
// rewritten (by MCP command_add, by the user, or by lerd's own writers).
// Next palette open or dropdown open will re-fetch.
wsMessage.subscribe((msg) => {
  if (msg?.sites !== undefined) {
    clearCommandsCache();
  }
});
